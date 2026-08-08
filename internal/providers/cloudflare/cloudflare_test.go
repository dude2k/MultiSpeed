package cloudflare

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/providers"
)

func TestNativeRunUsesBoundSourceAndMultipleSamples(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host, _, _ := net.SplitHostPort(r.RemoteAddr); host != "127.0.0.1" {
			t.Errorf("remote source=%q", host)
		}
		w.Header().Set("CF-Ray", "abc-TST")
		switch r.URL.Path {
		case "/cdn-cgi/trace":
			_, _ = io.WriteString(w, "ip=127.0.0.1\ncolo=TST\n")
		case "/__down":
			size, _ := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
			time.Sleep(12 * time.Millisecond)
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			_, _ = io.CopyN(w, zeroReader{}, size)
		case "/__up":
			size, _ := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
			read, err := io.Copy(io.Discard, r.Body)
			if err != nil || read != size {
				t.Errorf("upload read=%d err=%v want=%d", read, err, size)
			}
			time.Sleep(12 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	adapter := NewWithBaseURL(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := adapter.Run(ctx, providers.RunRequest{SourceIP: "127.0.0.1", IPFamily: "ipv4", Target: providers.TestTarget{SelectionMode: "automatic"}, Options: map[string]any{"maxPayloadBytes": float64(4 << 20)}})
	if err != nil {
		t.Fatal(err)
	}
	if result.DownloadBitsPerSecond == nil || *result.DownloadBitsPerSecond <= 0 || result.UploadBitsPerSecond == nil || *result.UploadBitsPerSecond <= 0 {
		t.Fatalf("invalid throughput: %#v", result)
	}
	if result.CloudflareColo != "TST" || result.PublicIP != "127.0.0.1" {
		t.Fatalf("metadata: %#v", result)
	}
	if result.RawResponse == "" {
		t.Fatal("raw diagnostic response is empty")
	}
}

func TestServerDuration(t *testing.T) {
	got := serverDuration([]string{"cfRequestDuration;dur=2.5, other;dur=99"})
	if got != 2500*time.Microsecond {
		t.Fatalf("serverDuration=%v", got)
	}
}

func TestServerDurationPrefersRequestTotalOverSpeedComponents(t *testing.T) {
	got := serverDuration([]string{"cfRequestDuration;dur=12.5, cfSpeedStepOne;dur=5, cfSpeedStepTwo;dur=7"})
	if got != 12_500*time.Microsecond {
		t.Fatalf("serverDuration=%v", got)
	}
	fallback := serverDuration([]string{"cfSpeedStepOne;dur=5, cfSpeedStepTwo;dur=7"})
	if fallback != 12*time.Millisecond {
		t.Fatalf("serverDuration fallback=%v", fallback)
	}
}

func TestUploadDoesNotSubtractCloudflareRequestBodyDuration(t *testing.T) {
	const size = int64(1_000_000)
	adapter := NewWithBaseURL("https://speed.example")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotFirstResponseByte != nil {
			time.Sleep(100 * time.Millisecond)
			trace.GotFirstResponseByte()
		}
		_, _ = io.Copy(io.Discard, request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Server-Timing": []string{"cfRequestDuration;dur=90"},
			},
			Body:    io.NopCloser(strings.NewReader("ok")),
			Request: request,
		}, nil
	})}
	bps, _, err := adapter.upload(context.Background(), client, size)
	if err != nil {
		t.Fatal(err)
	}
	// One megabyte over roughly 100 ms is about 80 Mbit/s. The old code
	// subtracted 90 ms and reported roughly 800 Mbit/s.
	if bps < 70_000_000 || bps > 100_000_000 {
		t.Fatalf("upload throughput=%f", bps)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestValidateRejectsFakeServer(t *testing.T) {
	err := New().Validate(context.Background(), providers.TestTarget{SelectionMode: "fixed", ServerID: "1"})
	if err == nil {
		t.Fatal("expected fixed server rejection")
	}
	_ = fmt.Sprint(err)
}

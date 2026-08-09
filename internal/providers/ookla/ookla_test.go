package ookla

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/providers"
)

func TestAvailabilityRechecksPersistedAcceptanceWithoutRestart(t *testing.T) {
	accepted := false
	adapter := NewWithAcceptanceSource("multispeed-test-missing-ookla", func(context.Context) (bool, error) {
		return accepted, nil
	}, nil)
	availability := adapter.Availability(context.Background())
	if availability.Available || availability.UnavailabilityReason != providers.UnavailabilityReasonPolicy || !strings.Contains(availability.Message, "record EULA acceptance") {
		t.Fatalf("unexpected unaccepted availability: %+v", availability)
	}
	accepted = true
	availability = adapter.Availability(context.Background())
	if availability.Available || availability.UnavailabilityReason != providers.UnavailabilityReasonRuntime || !strings.Contains(availability.Message, "executable was not found") {
		t.Fatalf("acceptance did not advance to binary validation: %+v", availability)
	}
}

func TestAvailabilityFailsClosedWhenAcceptanceCannotBeVerified(t *testing.T) {
	adapter := NewWithAcceptanceSource("speedtest", func(context.Context) (bool, error) {
		return false, errors.New("database unavailable")
	}, nil)
	availability := adapter.Availability(context.Background())
	if availability.Available || availability.UnavailabilityReason != providers.UnavailabilityReasonPolicy || !strings.Contains(availability.Message, "could not be verified") {
		t.Fatalf("unexpected availability: %+v", availability)
	}
}

func TestBaseArgsPrefersSourceAddressBecauseNetworkFlagsAreMutuallyExclusive(t *testing.T) {
	args := New("speedtest", true, nil).baseArgs("eth1", "2001:db8::10")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--ip=2001:db8::10") {
		t.Fatalf("arguments %q do not contain the selected source address", joined)
	}
	if strings.Contains(joined, "--interface=") {
		t.Fatalf("arguments %q combine Ookla's mutually exclusive network flags", joined)
	}
	for _, unsupported := range []string{"--ipv4", "--ipv6"} {
		if strings.Contains(joined, unsupported) {
			t.Fatalf("arguments %q contain unsupported flag %q", joined, unsupported)
		}
	}
}

func TestBaseArgsFallsBackToInterfaceWithoutSourceAddress(t *testing.T) {
	joined := strings.Join(New("speedtest", true, nil).baseArgs("eth1", ""), " ")
	if !strings.Contains(joined, "--interface=eth1") || strings.Contains(joined, "--ip=") {
		t.Fatalf("unexpected interface-only arguments %q", joined)
	}
}

func TestParseResult(t *testing.T) {
	raw := []byte(`{"type":"result","ping":{"latency":12.4,"jitter":1.2},"download":{"bandwidth":12500000,"bytes":1000},"upload":{"bandwidth":6250000,"bytes":500},"packetLoss":0.5,"interface":{"externalIp":"203.0.113.4"},"server":{"id":42,"host":"speed.example","name":"Example","sponsor":"ISP","location":"Berlin","country":"Germany"},"result":{"url":"https://results.example.test/1"}}`)
	result, err := parseResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.DownloadBitsPerSecond == nil || *result.DownloadBitsPerSecond != 100_000_000 {
		t.Fatalf("download=%v", result.DownloadBitsPerSecond)
	}
	if result.UploadBitsPerSecond == nil || *result.UploadBitsPerSecond != 50_000_000 {
		t.Fatalf("upload=%v", result.UploadBitsPerSecond)
	}
	if result.Server.ID != "42" || result.PublicIP != "203.0.113.4" {
		t.Fatalf("metadata=%#v", result)
	}
}

func TestParseResultRejectsMalformedJSON(t *testing.T) {
	if _, err := parseResult([]byte(`{"type":`)); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
func TestParseResultRejectsIncompleteOrInvalidMetrics(t *testing.T) {
	for _, raw := range []string{
		`{"type":"result"}`,
		`{"type":"result","ping":{"latency":1,"jitter":1},"download":{"bandwidth":-1,"bytes":1},"upload":{"bandwidth":1,"bytes":1}}`,
		`{"type":"result","ping":{"latency":1,"jitter":1},"download":{"bandwidth":1,"bytes":1},"upload":{"bandwidth":1,"bytes":1},"packetLoss":101}`,
	} {
		if _, err := parseResult([]byte(raw)); err == nil {
			t.Fatalf("expected invalid result to fail: %s", raw)
		}
	}
}
func TestNumericID(t *testing.T) {
	for value, want := range map[string]bool{"123": true, "": false, "12x": false, "-1": false} {
		if got := numericID(value); got != want {
			t.Errorf("numericID(%q)=%v want %v", value, got, want)
		}
	}
}

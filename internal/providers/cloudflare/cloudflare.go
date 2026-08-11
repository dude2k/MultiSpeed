// Package cloudflare implements a bounded native Cloudflare edge speed test.
package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
	internalnetwork "github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
)

const (
	defaultBaseURL     = "https://speed.cloudflare.com"
	methodologyVersion = "cloudflare-native-bounded-v2"
	requestUserAgent   = "MultiSpeed/network-test-v2"
	defaultPayloadCap  = int64(128 << 20)
	absolutePayloadCap = int64(512 << 20)
)

type Adapter struct{ baseURL string }

func New() *Adapter { return &Adapter{baseURL: defaultBaseURL} }
func NewWithBaseURL(baseURL string) *Adapter {
	return &Adapter{baseURL: strings.TrimRight(baseURL, "/")}
}
func (*Adapter) ID() models.ProviderID { return models.ProviderCloudflare }
func (*Adapter) DisplayName() string   { return "Cloudflare edge" }
func (*Adapter) Capabilities() providers.Capabilities {
	return providers.Capabilities{SourceAddressBinding: true, IPv4: true, IPv6: true, Jitter: true}
}
func (*Adapter) Availability(context.Context) providers.Availability {
	return providers.Availability{Available: true, Version: methodologyVersion, Message: "Native bounded methodology with automatic Cloudflare edge selection."}
}
func (*Adapter) Version(context.Context) (string, error) { return methodologyVersion, nil }
func (*Adapter) ListServers(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
	return []providers.Server{}, nil
}
func (*Adapter) Validate(_ context.Context, target providers.TestTarget) error {
	if target.SelectionMode != "" && target.SelectionMode != "automatic" {
		return fmt.Errorf("cloudflare supports only Automatic edge selection: %w", providers.ErrInvalidTarget)
	}
	if target.ServerID != "" || target.ServerURL != "" {
		return fmt.Errorf("cloudflare does not accept a server ID or custom URL: %w", providers.ErrInvalidTarget)
	}
	return nil
}

type measurement struct {
	LatencyMS       []float64 `json:"latencySamplesMs"`
	DownloadBPS     []float64 `json:"downloadSamplesBps"`
	UploadBPS       []float64 `json:"uploadSamplesBps"`
	DownloadBytes   int64     `json:"downloadBytes"`
	UploadBytes     int64     `json:"uploadBytes"`
	PublicIP        string    `json:"publicIp"`
	Colo            string    `json:"colo"`
	Methodology     string    `json:"methodology"`
	PayloadCapBytes int64     `json:"payloadCapBytes"`
}

func (a *Adapter) Run(ctx context.Context, request providers.RunRequest) (providers.ProviderResult, error) {
	if err := a.Validate(ctx, request.Target); err != nil {
		return providers.ProviderResult{}, err
	}
	sourceIP := net.ParseIP(request.SourceIP)
	if sourceIP == nil {
		return providers.ProviderResult{}, errors.New("cloudflare source IP is invalid")
	}
	client, closeTransport, err := boundClient(sourceIP, request.IPFamily)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("cloudflare source binding failed: %w", err)
	}
	defer closeTransport()
	payloadCap := optionInt64(request.Options, "maxPayloadBytes", defaultPayloadCap)
	if payloadCap < 4<<20 {
		payloadCap = 4 << 20
	}
	if payloadCap > absolutePayloadCap {
		payloadCap = absolutePayloadCap
	}
	started := time.Now()
	measurement := measurement{Methodology: methodologyVersion, PayloadCapBytes: payloadCap}

	publicIP, colo, err := a.trace(ctx, client)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("cloudflare bound path discovery failed: %w", err)
	}
	measurement.PublicIP, measurement.Colo = publicIP, colo
	for i := 0; i < 9; i++ {
		latency, observedColo, err := a.latency(ctx, client)
		if err != nil {
			return providers.ProviderResult{}, fmt.Errorf("cloudflare latency sample %d failed: %w", i+1, err)
		}
		measurement.LatencyMS = append(measurement.LatencyMS, latency)
		if measurement.Colo == "" {
			measurement.Colo = observedColo
		}
	}

	ladders := []int64{100_000, 100_000, 100_000, 1_000_000, 1_000_000, 1_000_000, 10_000_000, 10_000_000, 25_000_000, 25_000_000}
	for _, size := range ladders {
		if ctx.Err() != nil {
			return providers.ProviderResult{}, ctx.Err()
		}
		if measurement.DownloadBytes+size > payloadCap/2 {
			break
		}
		bps, count, observedColo, err := a.download(ctx, client, size)
		if err != nil {
			return providers.ProviderResult{}, fmt.Errorf("cloudflare download sample (%d bytes) failed: %w", size, err)
		}
		if bps > 0 {
			measurement.DownloadBPS = append(measurement.DownloadBPS, bps)
		}
		measurement.DownloadBytes += count
		if measurement.Colo == "" {
			measurement.Colo = observedColo
		}
	}
	for _, size := range ladders {
		if ctx.Err() != nil {
			return providers.ProviderResult{}, ctx.Err()
		}
		if measurement.UploadBytes+size > payloadCap/2 {
			break
		}
		bps, observedColo, err := a.upload(ctx, client, size)
		if err != nil {
			return providers.ProviderResult{}, fmt.Errorf("cloudflare upload sample (%d bytes) failed: %w", size, err)
		}
		if bps > 0 {
			measurement.UploadBPS = append(measurement.UploadBPS, bps)
		}
		measurement.UploadBytes += size
		if measurement.Colo == "" {
			measurement.Colo = observedColo
		}
	}
	if len(measurement.DownloadBPS) < 3 || len(measurement.UploadBPS) < 3 {
		return providers.ProviderResult{}, errors.New("cloudflare payload cap produced too few bandwidth samples")
	}

	latency := percentile(measurement.LatencyMS, 0.5)
	jitter := adjacentJitter(measurement.LatencyMS)
	down := int64(percentile(measurement.DownloadBPS, 0.90))
	up := int64(percentile(measurement.UploadBPS, 0.90))
	downBytes, upBytes := measurement.DownloadBytes, measurement.UploadBytes
	raw, _ := json.Marshal(measurement)
	return providers.ProviderResult{DownloadBitsPerSecond: &down, UploadBitsPerSecond: &up, LatencyMilliseconds: &latency,
		JitterMilliseconds: &jitter, DownloadBytes: &downBytes, UploadBytes: &upBytes, PublicIP: publicIP,
		CloudflareColo: measurement.Colo, DurationMilliseconds: time.Since(started).Milliseconds(), RawResponse: providers.SanitizeOutput(string(raw), providers.MaxStoredOutput), ProviderVersion: methodologyVersion}, nil
}

func boundClient(sourceIP net.IP, family string) (*http.Client, func(), error) {
	isIPv4 := sourceIP.To4() != nil
	if family == "ipv4" && !isIPv4 {
		return nil, nil, errors.New("ipv4 mode requires an IPv4 source address")
	}
	if family == "ipv6" && isIPv4 {
		return nil, nil, errors.New("ipv6 mode requires an IPv6 source address")
	}
	dialer, err := internalnetwork.NewSourceBoundDialer(sourceIP, 10*time.Second, 30*time.Second)
	if err != nil {
		return nil, nil, err
	}
	networkName := "tcp6"
	if isIPv4 {
		networkName = "tcp4"
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, networkName, address)
	},
		ForceAttemptHTTP2: true, DisableCompression: true, MaxIdleConns: 4, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2,
		TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections, nil
}

func (a *Adapter) trace(ctx context.Context, client *http.Client) (string, string, error) {
	response, err := do(ctx, client, http.MethodGet, a.baseURL+"/cdn-cgi/trace", nil, -1, nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("trace endpoint returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return "", "", err
	}
	publicIP, colo := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "ip" {
			publicIP = value
		}
		if key == "colo" {
			colo = value
		}
	}
	if net.ParseIP(publicIP) == nil {
		return "", colo, errors.New("trace response omitted a valid public IP")
	}
	return publicIP, colo, nil
}

func (a *Adapter) latency(ctx context.Context, client *http.Client) (float64, string, error) {
	started := time.Now()
	var firstByte time.Time
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Now() }}
	response, err := do(ctx, client, http.MethodGet, a.baseURL+"/__down?bytes=0", nil, -1, trace)
	if err != nil {
		return 0, "", err
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	closeErr := response.Body.Close()
	if readErr == nil {
		readErr = closeErr
	}
	if readErr != nil {
		return 0, "", readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, "", fmt.Errorf("latency endpoint returned HTTP %d", response.StatusCode)
	}
	if firstByte.IsZero() {
		return 0, "", errors.New("first response byte was not observed")
	}
	duration := firstByte.Sub(started) - serverDuration(response.Header.Values("Server-Timing"))
	if duration <= 0 {
		return 0, "", errors.New("invalid latency duration")
	}
	return float64(duration) / float64(time.Millisecond), coloFromResponse(response), nil
}

func (a *Adapter) download(ctx context.Context, client *http.Client, size int64) (float64, int64, string, error) {
	endpoint := a.baseURL + "/__down?bytes=" + strconv.FormatInt(size, 10)
	started := time.Now()
	var firstByte time.Time
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Now() }}
	response, err := do(ctx, client, http.MethodGet, endpoint, nil, -1, trace)
	if err != nil {
		return 0, 0, "", err
	}
	count, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, size+1))
	closeErr := response.Body.Close()
	if readErr == nil {
		readErr = closeErr
	}
	finished := time.Now()
	if readErr != nil {
		return 0, count, "", readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, count, "", fmt.Errorf("download endpoint returned HTTP %d", response.StatusCode)
	}
	if count != size {
		return 0, count, "", fmt.Errorf("expected %d bytes, received %d", size, count)
	}
	duration := finished.Sub(started) - serverDuration(response.Header.Values("Server-Timing"))
	if firstByte.IsZero() || duration < 10*time.Millisecond {
		return 0, count, coloFromResponse(response), nil
	}
	return float64(count*8) / duration.Seconds(), count, coloFromResponse(response), nil
}

func (a *Adapter) upload(ctx context.Context, client *http.Client, size int64) (float64, string, error) {
	endpoint := a.baseURL + "/__up?bytes=" + strconv.FormatInt(size, 10)
	started := time.Now()
	var firstByte time.Time
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Now() }}
	response, err := do(ctx, client, http.MethodPost, endpoint, io.LimitReader(zeroReader{}, size), size, trace)
	if err != nil {
		return 0, "", err
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	closeErr := response.Body.Close()
	if readErr == nil {
		readErr = closeErr
	}
	if readErr != nil {
		return 0, "", readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, "", fmt.Errorf("upload endpoint returned HTTP %d", response.StatusCode)
	}
	if firstByte.IsZero() {
		return 0, "", errors.New("first response byte was not observed")
	}
	// The upload Server-Timing returned by Cloudflare includes receipt of the request body.
	// Subtracting it removes the transfer itself and leaves approximately one
	// RTT, producing impossible throughput that scales with payload size.
	duration := firstByte.Sub(started)
	if duration < 10*time.Millisecond {
		return 0, coloFromResponse(response), nil
	}
	return float64(size*8) / duration.Seconds(), coloFromResponse(response), nil
}

func do(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, length int64, trace *httptrace.ClientTrace) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if trace != nil {
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", requestUserAgent)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/octet-stream")
		request.ContentLength = length
	}
	return client.Do(request)
}

func serverDuration(values []string) time.Duration {
	var requestMilliseconds float64
	var speedMilliseconds float64
	for _, header := range values {
		for _, entry := range strings.Split(header, ",") {
			parts := strings.Split(entry, ";")
			if len(parts) < 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			isRequestDuration := name == "cfReqDur" || name == "cfRequestDuration"
			isSpeedDuration := strings.HasPrefix(name, "cfSpeed")
			if !isRequestDuration && !isSpeedDuration {
				continue
			}
			for _, parameter := range parts[1:] {
				key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if ok && key == "dur" {
					if parsed, err := strconv.ParseFloat(strings.Trim(value, `"`), 64); err == nil && parsed > 0.01 {
						if isRequestDuration {
							requestMilliseconds += parsed
						} else {
							speedMilliseconds += parsed
						}
					}
				}
			}
		}
	}
	// Current responses can contain both the total request duration and its
	// cfSpeed components. Prefer the total; summing both double-counts time.
	if requestMilliseconds > 0 {
		return time.Duration(requestMilliseconds * float64(time.Millisecond))
	}
	return time.Duration(speedMilliseconds * float64(time.Millisecond))
}

func coloFromResponse(response *http.Response) string {
	ray := response.Header.Get("CF-Ray")
	if _, suffix, ok := strings.Cut(ray, "-"); ok {
		return strings.ToUpper(strings.TrimSpace(suffix))
	}
	return ""
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	position := quantile * float64(len(copyValues)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return copyValues[lower]
	}
	weight := position - float64(lower)
	return copyValues[lower]*(1-weight) + copyValues[upper]*weight
}

func adjacentJitter(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	total := 0.0
	for i := 1; i < len(values); i++ {
		total += math.Abs(values[i] - values[i-1])
	}
	return total / float64(len(values)-1)
}

func optionInt64(options map[string]any, key string, fallback int64) int64 {
	value, ok := options[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return fallback
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

var _ = url.QueryEscape
var _ sync.Mutex

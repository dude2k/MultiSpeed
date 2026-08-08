package librespeed

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type recordingRunner struct {
	requests []providerprocess.Request
	version  string
}

func (r *recordingRunner) Run(_ context.Context, request providerprocess.Request) (providerprocess.Result, error) {
	r.requests = append(r.requests, request)
	if len(request.Arguments) == 1 && request.Arguments[0] == "--version" {
		version := r.version
		if version == "" {
			version = "librespeed-cli v1.0.13+multispeed.dns2.xnet055"
		}
		return providerprocess.Result{Stdout: version}, nil
	}
	return providerprocess.Result{Stdout: `[{"timestamp":"2026-01-01T00:00:00Z","server":{"name":"Custom","url":"http://speed.example"},"client":{"ip":"203.0.113.10"},"bytes_sent":10,"bytes_received":20,"ping":5,"jitter":1,"upload":10,"download":20,"share":""}]`}, nil
}

func TestAvailabilityRejectsUnpatchedCLI(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{
		"librespeed-cli v1.0.13",
		"librespeed-cli v1.0.13+multispeed.dns1.xnet055",
	} {
		adapter := New(binary, &recordingRunner{version: version})
		availability := adapter.Availability(context.Background())
		if availability.Available || !strings.Contains(availability.Message, "source-bound DNS, destination pinning, or patched dependency marker") {
			t.Fatalf("version=%q availability=%+v", version, availability)
		}
	}
}

func TestCustomHTTPServerUsesSupportedCLIFlags(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	adapter := NewWithCustomServerURLPolicy(binary, runner, customURLPolicy(t, "http://speed.example"))
	adapter.resolveCustomServerIPs = func(context.Context, string, string, string) ([]string, error) {
		return []string{"192.0.2.80", "192.0.2.81"}, nil
	}
	result, err := adapter.Run(context.Background(), providers.RunRequest{
		SourceIP:       "192.0.2.10",
		IPFamily:       "ipv4",
		TimeoutSeconds: 30,
		Target: providers.TestTarget{
			SelectionMode:          "custom",
			ServerURL:              "http://speed.example",
			CustomServerDefinition: map[string]any{"allowInsecure": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var customRequest *providerprocess.Request
	for index := range runner.requests {
		if len(runner.requests[index].Stdin) > 0 {
			customRequest = &runner.requests[index]
			break
		}
	}
	if customRequest == nil {
		t.Fatal("custom request was not executed")
	}
	joined := strings.Join(customRequest.Arguments, " ")
	if strings.Contains(joined, "--insecure") {
		t.Fatalf("arguments %q contain an unsupported LibreSpeed flag", joined)
	}
	if !strings.Contains(joined, "--local-json - --server 1") {
		t.Fatalf("arguments %q do not select the local server definition", joined)
	}
	if got := customRequest.Environment[allowedServerEndpointsEnvironment]; got != "192.0.2.80:80,192.0.2.81:80" {
		t.Fatalf("pinned custom-server endpoints=%q", got)
	}
	if result.TLSVerificationDisabled {
		t.Fatal("plain HTTP opt-in must not be recorded as disabled TLS verification")
	}
}

func TestParseResultNormalizesBitsPerSecond(t *testing.T) {
	raw := []byte(`[{"timestamp":"2026-01-01T00:00:00Z","server":{"name":"Lab","url":"https://speed.example"},"client":{"ip":"203.0.113.9"},"bytes_sent":200,"bytes_received":400,"ping":10.5,"jitter":"1.25","upload":50.5,"download":"100.25"}]`)
	result, err := parseResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.DownloadBitsPerSecond == nil || *result.DownloadBitsPerSecond != 100_250_000 {
		t.Fatalf("download=%v", result.DownloadBitsPerSecond)
	}
	if result.UploadBitsPerSecond == nil || *result.UploadBitsPerSecond != 50_500_000 {
		t.Fatalf("upload=%v", result.UploadBitsPerSecond)
	}
	if result.JitterMilliseconds == nil || *result.JitterMilliseconds != 1.25 {
		t.Fatalf("jitter=%v", result.JitterMilliseconds)
	}
	if result.Server.Host != "speed.example" {
		t.Fatalf("server host=%q", result.Server.Host)
	}
}

func TestParseServersFromCLIList(t *testing.T) {
	raw := []byte("51: Amsterdam, Netherlands (Clouvider) (https://ams.speedtest.clouvider.net/backend)  [Sponsor: Clouvider @ https://www.clouvider.co.uk/]\n94: Amsterdam, Netherlands (Sharktech) (https://amsspeed.sharktech.net) \n")
	servers, err := parseServers(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ID != "51" || servers[0].Sponsor != "Clouvider" || servers[0].Host != "ams.speedtest.clouvider.net" {
		t.Fatalf("unexpected first server: %#v", servers)
	}
	if servers[1].ID != "94" || servers[1].Sponsor != "" || servers[1].URL != "https://amsspeed.sharktech.net" {
		t.Fatalf("unexpected second server: %#v", servers[1])
	}
}

func TestParseResultRejectsMultipleResults(t *testing.T) {
	raw := []byte(`[{"download":1,"upload":1,"ping":1},{"download":2,"upload":2,"ping":2}]`)
	if _, err := parseResult(raw); err == nil {
		t.Fatal("expected multiple results to be rejected")
	}
}

func TestParseResultRejectsNonFiniteAndNegativeMetrics(t *testing.T) {
	for _, raw := range []string{
		`[{"download":"NaN","upload":2,"ping":3}]`,
		`[{"download":1,"upload":-2,"ping":3}]`,
		`[{"download":1,"upload":2,"ping":"Inf"}]`,
	} {
		if _, err := parseResult([]byte(raw)); err == nil {
			t.Fatalf("expected invalid metrics to fail: %s", raw)
		}
	}
}
func TestCustomHTTPRequiresExplicitOptIn(t *testing.T) {
	adapter := NewWithCustomServerURLPolicy("unused", nil, customURLPolicy(t, "http://speed.lan"))
	target := providers.TestTarget{SelectionMode: "custom", ServerURL: "http://speed.lan"}
	if err := adapter.Validate(context.Background(), target); err == nil {
		t.Fatal("expected insecure URL rejection")
	}
	target.CustomServerDefinition = map[string]any{"allowInsecure": true}
	if err := adapter.Validate(context.Background(), target); err != nil {
		t.Fatalf("explicit insecure URL rejected: %v", err)
	}
}
func TestCustomDefinitionRejectsUnknownAndAbsoluteEndpointFields(t *testing.T) {
	adapter := NewWithCustomServerURLPolicy("unused", nil, customURLPolicy(t, "http://speed.lan"))
	for name, definition := range map[string]map[string]any{
		"unknown field":           {"allowInsecure": true, "command": "ignored"},
		"absolute endpoint":       {"allowInsecure": true, "dlURL": "https://other.example/garbage"},
		"network-path endpoint":   {"allowInsecure": true, "dlURL": "//other.example/garbage"},
		"backslash endpoint":      {"allowInsecure": true, "dlURL": `\\other.example\garbage`},
		"encoded-slash endpoint":  {"allowInsecure": true, "dlURL": "%2f%2fother.example/garbage"},
		"parent-segment endpoint": {"allowInsecure": true, "dlURL": "../garbage"},
		"absolute-path endpoint":  {"allowInsecure": true, "dlURL": "/garbage"},
		"wrong opt-in type":       {"allowInsecure": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			target := providers.TestTarget{SelectionMode: "custom", ServerURL: "http://speed.lan", CustomServerDefinition: definition}
			if err := adapter.Validate(context.Background(), target); err == nil {
				t.Fatal("invalid custom definition was accepted")
			}
		})
	}
}

func TestCustomServerURLMustBeOperatorAuthorizedBeforeResolutionOrRun(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	adapter := New(binary, runner)
	var resolutionCalls int
	adapter.resolveCustomServerIPs = func(context.Context, string, string, string) ([]string, error) {
		resolutionCalls++
		return []string{"127.0.0.1"}, nil
	}
	target := providers.TestTarget{SelectionMode: "custom", ServerURL: "https://metadata.example.test"}
	if err := adapter.Validate(context.Background(), target); err == nil {
		t.Fatal("unapproved custom URL passed validation")
	}
	if _, err := adapter.Run(context.Background(), providers.RunRequest{SourceIP: "192.0.2.10", IPFamily: "ipv4", Target: target}); err == nil {
		t.Fatal("unapproved custom URL reached provider execution")
	}
	if resolutionCalls != 0 || len(runner.requests) != 1 { // Availability performs only the fixed --version check.
		t.Fatalf("unapproved target side effects: resolutions=%d requests=%+v", resolutionCalls, runner.requests)
	}
}

func TestResolveCustomServerIPsAcceptsOnlyTheSelectedSourceFamily(t *testing.T) {
	for _, test := range []struct {
		name, rawURL, source, family, want string
		wantError                          bool
	}{
		{name: "IPv4", rawURL: "https://192.0.2.80:8443/base", source: "192.0.2.10", family: "ipv4", want: "192.0.2.80"},
		{name: "IPv6", rawURL: "https://[2001:db8::80]:8443/base", source: "2001:db8::10", family: "ipv6", want: "2001:db8::80"},
		{name: "family mismatch", rawURL: "https://[2001:db8::80]/", source: "192.0.2.10", family: "ipv4", wantError: true},
		{name: "unspecified target", rawURL: "https://0.0.0.0/", source: "192.0.2.10", family: "ipv4", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			addresses, err := resolveCustomServerIPs(context.Background(), test.rawURL, test.source, test.family)
			if test.wantError {
				if err == nil {
					t.Fatalf("addresses=%v", addresses)
				}
				return
			}
			if err != nil || len(addresses) != 1 || addresses[0] != test.want {
				t.Fatalf("addresses=%v error=%v, want %q", addresses, err, test.want)
			}
		})
	}
}

func TestCustomServerEndpointsPinTheAuthorizedPort(t *testing.T) {
	for _, test := range []struct {
		name, rawURL string
		addresses    []string
		want         string
	}{
		{name: "HTTPS default", rawURL: "https://speed.example.test/base", addresses: []string{"192.0.2.80"}, want: "192.0.2.80:443"},
		{name: "HTTP explicit", rawURL: "http://speed.example.test:8080/base", addresses: []string{"192.0.2.80"}, want: "192.0.2.80:8080"},
		{name: "IPv6 explicit", rawURL: "https://[2001:db8::80]:8443/base", addresses: []string{"2001:db8::80"}, want: "[2001:db8::80]:8443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoints, err := customServerEndpoints(test.rawURL, test.addresses)
			if err != nil || len(endpoints) != 1 || endpoints[0] != test.want {
				t.Fatalf("endpoints=%v error=%v, want %q", endpoints, err, test.want)
			}
		})
	}
	for _, test := range []struct {
		name, rawURL string
		addresses    []string
	}{
		{name: "empty addresses", rawURL: "https://speed.example.test", addresses: nil},
		{name: "invalid address", rawURL: "https://speed.example.test", addresses: []string{"not-an-ip"}},
		{name: "zero port", rawURL: "https://speed.example.test:0", addresses: []string{"192.0.2.80"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if endpoints, err := customServerEndpoints(test.rawURL, test.addresses); err == nil {
				t.Fatalf("endpoints=%v unexpectedly succeeded", endpoints)
			}
		})
	}
}

func customURLPolicy(t *testing.T, entries ...string) providers.CustomServerURLPolicy {
	t.Helper()
	policy, err := providers.NewCustomServerURLPolicy(entries)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
func TestParseResultRejectsMissingMetrics(t *testing.T) {
	if _, err := parseResult([]byte(`{"server":{}}`)); err == nil {
		t.Fatal("expected missing metric error")
	}
}

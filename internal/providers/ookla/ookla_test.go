package ookla

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type recordingRunner struct {
	requests []providerprocess.Request
}

func (r *recordingRunner) Run(_ context.Context, request providerprocess.Request) (providerprocess.Result, error) {
	r.requests = append(r.requests, request)
	if slicesContain(request.Arguments, "--version") {
		return providerprocess.Result{Stdout: "Speedtest by Ookla 1.2.0.84"}, nil
	}
	return providerprocess.Result{Stdout: `{"type":"result","ping":{"latency":12.4,"jitter":1.2},"download":{"bandwidth":12500000,"bytes":1000},"upload":{"bandwidth":6250000,"bytes":500},"packetLoss":0.5,"interface":{"externalIp":"203.0.113.4"},"server":{"id":42,"host":"speed.example","name":"Example","sponsor":"ISP","location":"Berlin","country":"Germany"},"result":{"url":"https://results.example.test/1"}}`}, nil
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestAvailabilityRechecksPersistedAcceptanceWithoutRestart(t *testing.T) {
	accepted := false
	adapter := NewWithAcceptanceSource("multispeed-test-missing-ookla", func(context.Context) (bool, error) {
		return accepted, nil
	}, nil)
	availability := adapter.Availability(context.Background())
	if availability.Available || availability.UnavailabilityReason != providers.UnavailabilityReasonPolicy || !strings.Contains(availability.Message, "record the technical acknowledgement") {
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
	for _, required := range []string{"--accept-license", "--accept-gdpr"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("arguments %q do not contain required operator-authorized flag %q", joined, required)
		}
	}
}

func TestBaseArgsFallsBackToInterfaceWithoutSourceAddress(t *testing.T) {
	joined := strings.Join(New("speedtest", true, nil).baseArgs("eth1", ""), " ")
	if !strings.Contains(joined, "--interface=eth1") || strings.Contains(joined, "--ip=") {
		t.Fatalf("unexpected interface-only arguments %q", joined)
	}
}

func TestRunUsesPrivatePersistentHomePerWANPath(t *testing.T) {
	runner := &recordingRunner{}
	runtimeDirectory := filepath.Join(t.TempDir(), "runtime")
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory("/bin/true", func(context.Context) (bool, error) { return true, nil }, runtimeDirectory, runner)
	for _, sourceIP := range []string{"2001:db8::10", "2001:db8::11"} {
		if _, err := adapter.Run(context.Background(), providers.RunRequest{InterfaceName: "eth0", SourceIP: sourceIP, Target: providers.TestTarget{SelectionMode: "automatic"}}); err != nil {
			t.Fatal(err)
		}
	}
	homes := make([]string, 0, 2)
	for _, request := range runner.requests {
		if slicesContain(request.Arguments, "--format=json") {
			homes = append(homes, request.HomeDirectory)
		}
	}
	if len(homes) != 2 || homes[0] == "" || homes[0] == homes[1] {
		t.Fatalf("per-path homes=%q", homes)
	}
	for _, homeDirectory := range homes {
		if filepath.Dir(homeDirectory) != runtimeDirectory {
			t.Fatalf("home %q escaped runtime directory %q", homeDirectory, runtimeDirectory)
		}
		info, err := os.Stat(homeDirectory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("home %q info=%v error=%v", homeDirectory, info, err)
		}
	}
}

func TestRuntimeHomeCanonicalizesEquivalentSourceAddresses(t *testing.T) {
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory("speedtest", func(context.Context) (bool, error) { return true, nil }, filepath.Join(t.TempDir(), "runtime"), &recordingRunner{})
	first, err := adapter.prepareRuntimeHome("eth0", "2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.prepareRuntimeHome("eth0", "2001:0db8:0:0:0:0:0:1")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("equivalent source addresses used different homes: %q != %q", first, second)
	}
}

func TestFailureMessageExtractsActionableDiagnosticsWithoutLegalBannerNoise(t *testing.T) {
	stderr := `==============================================================================
You may only use this software subject to its applicable terms.
==============================================================================
{"type":"log","message":"Failed to save settings: permission denied","level":"error"}
{"type":"log","message":"Failed to save settings: permission denied","level":"error"}
{"type":"log","message":"Error: [0] Timeout occurred in connect.","level":"error"}`
	message := failureMessage(stderr)
	for _, expected := range []string{"managed runtime directory", "After fixing storage", "gateway", "Timeout occurred in connect"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("failure message %q does not contain %q", message, expected)
		}
	}
	if strings.Contains(message, "You may only use") || strings.Count(message, "Failed to save settings") != 1 {
		t.Fatalf("failure message retained banner or duplicate diagnostics: %q", message)
	}
}

func TestFailureMessageExplainsRuntimeDirectoryPermissions(t *testing.T) {
	message := failureMessage(`{"type":"log","message":"Failed to save settings: permission denied","level":"error"}`)
	for _, expected := range []string{"managed runtime directory", "APP_DATA_DIR", "UID 10001"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("failure message %q does not contain %q", message, expected)
		}
	}
}

func TestFailureMessageDoesNotMisclassifyUnrelatedPermissionFailure(t *testing.T) {
	message := failureMessage(`{"type":"log","message":"bind socket: permission denied","level":"error"}`)
	if strings.Contains(message, "managed runtime directory") || !strings.Contains(message, "bind socket") {
		t.Fatalf("unexpected diagnostic: %q", message)
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

//go:build linux

package process

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunnerCancelsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := (ExecRunner{}).Run(ctx, Request{Binary: "/bin/sh", Arguments: []string{"-c", "sleep 10 & wait"}, OutputLimit: 4096})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error=%v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("cancellation took %v", time.Since(started))
	}
}
func TestRunnerBoundsOutput(t *testing.T) {
	_, err := (ExecRunner{}).Run(context.Background(), Request{Binary: "/bin/sh", Arguments: []string{"-c", "yes x | head -c 10000"}, OutputLimit: 1024})
	if err == nil {
		t.Fatal("expected output limit error")
	}
}

func TestRunnerScopesAndOverridesProviderEnvironment(t *testing.T) {
	t.Setenv("MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS", "untrusted-inherited-value")
	result, err := (ExecRunner{}).Run(context.Background(), Request{
		Binary: "/bin/sh", Arguments: []string{"-c", `printf '%s' "$MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS"`}, OutputLimit: 4096,
		Environment: map[string]string{"MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS": "192.0.2.10:443,[2001:db8::10]:443"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "192.0.2.10:443,[2001:db8::10]:443" {
		t.Fatalf("provider environment=%q", result.Stdout)
	}
	for _, environment := range []map[string]string{
		{"PATH": "/tmp"},
		{"MULTISPEED_PROVIDER_bad": "value"},
		{"MULTISPEED_PROVIDER_VALUE": "value\nwith-control"},
	} {
		if _, err := (ExecRunner{}).Run(context.Background(), Request{Binary: "/bin/true", Environment: environment}); err == nil {
			t.Fatalf("unsafe provider environment %#v was accepted", environment)
		}
	}
	t.Setenv("MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS", "must-not-leak")
	result, err = (ExecRunner{}).Run(context.Background(), Request{
		Binary: "/bin/sh", Arguments: []string{"-c", `printf '%s' "${MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS-unset}"`}, OutputLimit: 4096,
	})
	if err != nil || result.Stdout != "unset" {
		t.Fatalf("inherited provider guard leaked: output=%q error=%v", result.Stdout, err)
	}
}

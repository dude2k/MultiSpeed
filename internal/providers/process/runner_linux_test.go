//go:build linux

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRunnerProvidesAnIsolatedWritableHomeDirectory(t *testing.T) {
	homeDirectory := t.TempDir()
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/.config")
	result, err := (ExecRunner{}).Run(context.Background(), Request{
		Binary:        "/bin/sh",
		Arguments:     []string{"-c", `mkdir -p "$XDG_CONFIG_HOME/ookla" && printf '%s\n%s' "$HOME" "$XDG_CONFIG_HOME" && printf settings > "$XDG_CONFIG_HOME/ookla/speedtest-cli.json"`},
		OutputLimit:   4096,
		HomeDirectory: homeDirectory,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := homeDirectory + "\n" + filepath.Join(homeDirectory, ".config")
	if result.Stdout != want {
		t.Fatalf("provider home output=%q want %q", result.Stdout, want)
	}
	if contents, err := os.ReadFile(filepath.Join(homeDirectory, ".config", "ookla", "speedtest-cli.json")); err != nil || string(contents) != "settings" {
		t.Fatalf("provider could not persist settings: contents=%q error=%v", contents, err)
	}
}

func TestRunnerRejectsUnsafeHomeDirectories(t *testing.T) {
	for _, homeDirectory := range []string{"relative", "../escape", "/tmp/unsafe\nvalue", strings.Repeat("x", maxArgumentLength+1)} {
		if _, err := (ExecRunner{}).Run(context.Background(), Request{Binary: "/bin/true", HomeDirectory: homeDirectory}); err == nil {
			t.Fatalf("unsafe provider home %q was accepted", homeDirectory)
		}
	}
}

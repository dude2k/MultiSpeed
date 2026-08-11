//go:build linux

package ookla

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type controlledRunner struct {
	started chan string
	release chan struct{}
}

func (runner *controlledRunner) Run(ctx context.Context, request providerprocess.Request) (providerprocess.Result, error) {
	select {
	case runner.started <- request.HomeDirectory:
	case <-ctx.Done():
		return providerprocess.Result{}, ctx.Err()
	}
	select {
	case <-runner.release:
		return providerprocess.Result{}, nil
	case <-ctx.Done():
		return providerprocess.Result{}, ctx.Err()
	}
}

func TestAdapterAndExecRunnerPersistSettingsOutsideInheritedNonexistentHome(t *testing.T) {
	t.Setenv("HOME", "/nonexistent")
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/.config")
	binary := filepath.Join(t.TempDir(), "speedtest")
	fixture := `#!/bin/sh
set -eu
case " $* " in
  *" --version "*)
    printf '%s\n' 'Speedtest by Ookla 1.2.0 integration-fixture'
    ;;
  *)
    mkdir -p "$HOME/.config/ookla"
    printf '%s' accepted > "$HOME/.config/ookla/speedtest-cli.json"
    printf '%s\n' '{"type":"result","ping":{"latency":12.4,"jitter":1.2},"download":{"bandwidth":12500000,"bytes":1000},"upload":{"bandwidth":6250000,"bytes":500},"packetLoss":0.5,"interface":{"externalIp":"203.0.113.4"},"server":{"id":42,"host":"speed.example","name":"Example","sponsor":"ISP","location":"Berlin","country":"Germany"},"result":{"url":"https://results.example.test/1"}}'
    ;;
esac
`
	if err := os.WriteFile(binary, []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDirectory := filepath.Join(t.TempDir(), "runtime")
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory(binary, func(context.Context) (bool, error) { return true, nil }, runtimeDirectory, providerprocess.ExecRunner{})
	request := providers.RunRequest{InterfaceName: "eth0", SourceIP: "192.0.2.10", Target: providers.TestTarget{SelectionMode: "automatic"}}
	if _, err := adapter.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	homeDirectory, err := adapter.prepareRuntimeHome(request.InterfaceName, request.SourceIP)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(homeDirectory, ".config", "ookla", "speedtest-cli.json"))
	if err != nil || string(contents) != "accepted" {
		t.Fatalf("persisted settings=%q error=%v", contents, err)
	}
	if strings.HasPrefix(homeDirectory, "/nonexistent") {
		t.Fatalf("managed runtime home unexpectedly used inherited HOME: %q", homeDirectory)
	}
}

func TestRuntimeHomeRejectsSymlinkedAncestor(t *testing.T) {
	base := t.TempDir()
	realDirectory := filepath.Join(base, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(base, "linked")
	if err := os.Symlink(realDirectory, symlink); err != nil {
		t.Fatal(err)
	}
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory("speedtest", func(context.Context) (bool, error) { return true, nil }, filepath.Join(symlink, "runtime"), &recordingRunner{})
	if _, err := adapter.prepareRuntimeHome("eth0", "192.0.2.10"); err == nil || !strings.Contains(err.Error(), "real directories") {
		t.Fatalf("symlinked runtime ancestor error=%v", err)
	}
}

func TestAdapterSerializesSameHomeButAllowsDifferentWANHomes(t *testing.T) {
	runner := &controlledRunner{started: make(chan string, 4), release: make(chan struct{}, 4)}
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory("speedtest", func(context.Context) (bool, error) { return true, nil }, t.TempDir(), runner)
	run := func(home string) <-chan error {
		result := make(chan error, 1)
		go func() {
			_, err := adapter.runProcess(context.Background(), providerprocess.Request{Binary: "/bin/true", HomeDirectory: home})
			result <- err
		}()
		return result
	}

	first := run("/tmp/same-home")
	if started := <-runner.started; started != "/tmp/same-home" {
		t.Fatalf("first home=%q", started)
	}
	second := run("/tmp/same-home")
	select {
	case started := <-runner.started:
		t.Fatalf("same home started concurrently: %q", started)
	case <-time.After(100 * time.Millisecond):
	}
	runner.release <- struct{}{}
	if started := <-runner.started; started != "/tmp/same-home" {
		t.Fatalf("second home=%q", started)
	}
	runner.release <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}

	wanA := run("/tmp/wan-a")
	wanB := run("/tmp/wan-b")
	seen := map[string]bool{}
	for range 2 {
		select {
		case started := <-runner.started:
			seen[started] = true
		case <-time.After(time.Second):
			t.Fatal("different WAN homes did not run concurrently")
		}
	}
	if !seen["/tmp/wan-a"] || !seen["/tmp/wan-b"] {
		t.Fatalf("concurrent homes=%v", seen)
	}
	runner.release <- struct{}{}
	runner.release <- struct{}{}
	if err := <-wanA; err != nil {
		t.Fatal(err)
	}
	if err := <-wanB; err != nil {
		t.Fatal(err)
	}
	adapter.runtimeLocksMu.Lock()
	remaining := len(adapter.runtimeLocks)
	adapter.runtimeLocksMu.Unlock()
	if remaining != 0 {
		t.Fatalf("completed runtime locks were retained: %d", remaining)
	}
}

func TestCanceledRuntimeLockWaiterIsRemoved(t *testing.T) {
	runner := &controlledRunner{started: make(chan string, 2), release: make(chan struct{}, 2)}
	adapter := NewWithAcceptanceSourceAndRuntimeDirectory("speedtest", func(context.Context) (bool, error) { return true, nil }, t.TempDir(), runner)
	firstResult := make(chan error, 1)
	go func() {
		_, err := adapter.runProcess(context.Background(), providerprocess.Request{Binary: "/bin/true", HomeDirectory: "/tmp/same-home"})
		firstResult <- err
	}()
	<-runner.started

	waitContext, cancel := context.WithCancel(context.Background())
	waitResult := make(chan error, 1)
	go func() {
		_, err := adapter.runProcess(waitContext, providerprocess.Request{Binary: "/bin/true", HomeDirectory: "/tmp/same-home"})
		waitResult <- err
	}()
	cancel()
	if err := <-waitResult; err == nil {
		t.Fatal("canceled runtime-lock waiter unexpectedly succeeded")
	}
	runner.release <- struct{}{}
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	adapter.runtimeLocksMu.Lock()
	remaining := len(adapter.runtimeLocks)
	adapter.runtimeLocksMu.Unlock()
	if remaining != 0 {
		t.Fatalf("canceled runtime lock was retained: %d", remaining)
	}
}

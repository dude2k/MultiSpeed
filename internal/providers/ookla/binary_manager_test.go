package ookla

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryManagerInstallsValidatedELFAtomically(t *testing.T) {
	dataDirectory := t.TempDir()
	binaryPath := filepath.Join(dataDirectory, "providers", "ookla", "speedtest")
	verifierCalls := 0
	manager := NewBinaryManager(dataDirectory, binaryPath, true, BinaryVerifierFunc(func(_ context.Context, path string) (string, error) {
		verifierCalls++
		if path == binaryPath || !strings.HasPrefix(filepath.Base(path), ".speedtest-upload-") {
			t.Fatalf("verifier received non-temporary path %q", path)
		}
		return "Speedtest by Ookla 1.2.0.84", nil
	}))

	result, err := manager.Install(context.Background(), bytes.NewReader(testAMD64ELF()))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if verifierCalls != 1 || !result.UploadEnabled || !result.Installed || result.Version != "Speedtest by Ookla 1.2.0.84" || len(result.SHA256) != 64 {
		t.Fatalf("unexpected result: %+v calls=%d", result, verifierCalls)
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
		t.Fatalf("installed mode=%v", info.Mode())
	}
	if temporary, _ := filepath.Glob(filepath.Join(filepath.Dir(binaryPath), ".speedtest-upload-*")); len(temporary) != 0 {
		t.Fatalf("temporary uploads remain: %v", temporary)
	}
}

func TestBinaryManagerRejectsInvalidFileWithoutReplacingExistingBinary(t *testing.T) {
	dataDirectory := t.TempDir()
	binaryPath := filepath.Join(dataDirectory, "providers", "ookla", "speedtest")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("existing"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewBinaryManager(dataDirectory, binaryPath, true, BinaryVerifierFunc(func(context.Context, string) (string, error) {
		t.Fatal("invalid ELF reached verifier")
		return "", nil
	}))
	if _, err := manager.Install(context.Background(), strings.NewReader("not an executable")); !errors.Is(err, ErrInvalidBinary) {
		t.Fatalf("Install invalid file error=%v", err)
	}
	contents, err := os.ReadFile(binaryPath)
	if err != nil || string(contents) != "existing" {
		t.Fatalf("existing binary changed: %q, %v", contents, err)
	}
}

func TestBinaryManagerFailsClosedOutsideDataDirectoryOrWhenDisabled(t *testing.T) {
	dataDirectory := t.TempDir()
	for name, manager := range map[string]*BinaryManager{
		"outside":          NewBinaryManager(dataDirectory, filepath.Join(filepath.Dir(dataDirectory), "speedtest"), true, nil),
		"database":         NewBinaryManager(dataDirectory, filepath.Join(dataDirectory, "multispeed.db"), true, nil),
		"runtime subtree":  NewBinaryManager(dataDirectory, filepath.Join(dataDirectory, "providers", "ookla", "runtime", "speedtest"), true, nil),
		"alternate nested": NewBinaryManager(dataDirectory, filepath.Join(dataDirectory, "providers", "other", "speedtest"), true, nil),
		"disabled":         NewBinaryManager(dataDirectory, filepath.Join(dataDirectory, "providers", "ookla", "speedtest"), false, nil),
	} {
		t.Run(name, func(t *testing.T) {
			if manager.Status().UploadEnabled {
				t.Fatal("upload unexpectedly enabled")
			}
			if _, err := manager.Install(context.Background(), bytes.NewReader(testAMD64ELF())); !errors.Is(err, ErrBinaryUploadDisabled) {
				t.Fatalf("Install error=%v", err)
			}
		})
	}
}

func TestBinaryManagerRejectsDirectoryAtManagedFilePath(t *testing.T) {
	dataDirectory := t.TempDir()
	binaryPath := filepath.Join(dataDirectory, "providers", "ookla", "speedtest")
	if err := os.MkdirAll(binaryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewBinaryManager(dataDirectory, binaryPath, true, BinaryVerifierFunc(func(context.Context, string) (string, error) {
		t.Fatal("path conflict reached verifier")
		return "", nil
	}))
	if _, err := manager.Install(context.Background(), bytes.NewReader(testAMD64ELF())); !errors.Is(err, ErrBinaryPathConflict) {
		t.Fatalf("Install path-conflict error=%v", err)
	}
}

func TestCLIBinaryVerifierUsesManagedHome(t *testing.T) {
	homeDirectory := filepath.Join(t.TempDir(), "runtime", "upload-verification")
	runner := &recordingRunner{}
	verifier := cliBinaryVerifier{runner: runner, homeDirectory: homeDirectory}
	version, err := verifier.Verify(context.Background(), "/tmp/speedtest")
	if err != nil {
		t.Fatal(err)
	}
	if version != "Speedtest by Ookla 1.2.0.84" || len(runner.requests) != 1 || runner.requests[0].HomeDirectory != homeDirectory {
		t.Fatalf("version=%q requests=%+v", version, runner.requests)
	}
	if info, err := os.Stat(homeDirectory); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("verification home info=%v error=%v", info, err)
	}
}

func testAMD64ELF() []byte {
	header := make([]byte, 64)
	copy(header, []byte("\x7fELF"))
	header[4] = 2
	header[5] = 1
	header[6] = 1
	binary.LittleEndian.PutUint16(header[16:18], 3)
	binary.LittleEndian.PutUint16(header[18:20], 62)
	return header
}

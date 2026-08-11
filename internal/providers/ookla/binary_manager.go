package ookla

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

const MaxBinaryUploadBytes int64 = 64 << 20

var (
	ErrBinaryUploadDisabled = errors.New("ookla binary upload is disabled")
	ErrBinaryTooLarge       = errors.New("ookla binary exceeds the upload limit")
	ErrInvalidBinary        = errors.New("uploaded file is not a compatible Ookla executable")
	ErrBinaryPathConflict   = errors.New("managed Ookla binary path must be absent or a regular file")
)

type BinaryStatus struct {
	UploadEnabled  bool   `json:"uploadEnabled"`
	Installed      bool   `json:"installed"`
	MaxUploadBytes int64  `json:"maxUploadBytes"`
	Message        string `json:"message"`
}

type BinaryInstallResult struct {
	BinaryStatus
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type BinaryVerifier interface {
	Verify(context.Context, string) (string, error)
}

type BinaryVerifierFunc func(context.Context, string) (string, error)

func (verify BinaryVerifierFunc) Verify(ctx context.Context, path string) (string, error) {
	return verify(ctx, path)
}

type cliBinaryVerifier struct {
	runner        providerprocess.Runner
	homeDirectory string
}

func (verify cliBinaryVerifier) Verify(ctx context.Context, path string) (string, error) {
	if verify.homeDirectory != "" {
		if err := ensurePrivateDirectory(verify.homeDirectory); err != nil {
			return "", fmt.Errorf("prepare Ookla verification state: %w", err)
		}
	}
	result, err := verify.runner.Run(ctx, providerprocess.Request{Binary: path, Arguments: []string{"--version"}, OutputLimit: 16 << 10, HomeDirectory: verify.homeDirectory})
	if err != nil {
		return "", err
	}
	version := providers.SanitizeOutput(result.Stdout, 512)
	text := strings.ToLower(version)
	if version == "" || !strings.Contains(text, "speedtest") || !strings.Contains(text, "ookla") {
		return "", errors.New("version output did not identify Speedtest by Ookla")
	}
	return version, nil
}

type BinaryManager struct {
	mu       sync.Mutex
	dataDir  string
	path     string
	enabled  bool
	managed  bool
	verifier BinaryVerifier
}

func NewBinaryManager(dataDirectory, binaryPath string, enabled bool, verifier BinaryVerifier) *BinaryManager {
	dataDirectory, dataErr := filepath.Abs(filepath.Clean(dataDirectory))
	binaryPath, binaryErr := filepath.Abs(filepath.Clean(binaryPath))
	managedPath := filepath.Join(dataDirectory, "providers", "ookla", "speedtest")
	managed := dataErr == nil && binaryErr == nil && binaryPath == managedPath
	if verifier == nil {
		verifier = cliBinaryVerifier{
			runner:        providerprocess.ExecRunner{},
			homeDirectory: filepath.Join(dataDirectory, "providers", "ookla", "runtime", "upload-verification"),
		}
	}
	return &BinaryManager{dataDir: dataDirectory, path: binaryPath, enabled: enabled, managed: managed, verifier: verifier}
}

func (manager *BinaryManager) Status() BinaryStatus {
	status := BinaryStatus{UploadEnabled: manager.enabled && manager.managed, MaxUploadBytes: MaxBinaryUploadBytes}
	switch {
	case !manager.enabled:
		status.Message = "Manual Ookla executable upload is disabled by this deployment."
	case !manager.managed:
		status.Message = "Manual upload is available only at APP_DATA_DIR/providers/ookla/speedtest; use an external read-only mount without upload for every other OOKLA_BINARY path."
	default:
		status.Message = "Upload a separately obtained Linux amd64 Speedtest by Ookla executable."
	}
	if info, err := os.Stat(manager.path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o100 != 0 {
		status.Installed = true
	}
	return status
}

func (manager *BinaryManager) Install(ctx context.Context, source io.Reader) (BinaryInstallResult, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.enabled || !manager.managed {
		return BinaryInstallResult{}, ErrBinaryUploadDisabled
	}
	if source == nil {
		return BinaryInstallResult{}, fmt.Errorf("%w: file body is empty", ErrInvalidBinary)
	}
	if err := manager.prepareParent(); err != nil {
		return BinaryInstallResult{}, err
	}
	if info, err := os.Lstat(manager.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return BinaryInstallResult{}, ErrBinaryPathConflict
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return BinaryInstallResult{}, fmt.Errorf("inspect managed Ookla executable: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(manager.path), ".speedtest-upload-*")
	if err != nil {
		return BinaryInstallResult{}, fmt.Errorf("create Ookla upload: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o700); err != nil {
		return BinaryInstallResult{}, fmt.Errorf("secure Ookla upload permissions: %w", err)
	}

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, MaxBinaryUploadBytes+1))
	if err != nil {
		return BinaryInstallResult{}, fmt.Errorf("read Ookla upload: %w", err)
	}
	if written == 0 {
		return BinaryInstallResult{}, fmt.Errorf("%w: file body is empty", ErrInvalidBinary)
	}
	if written > MaxBinaryUploadBytes {
		return BinaryInstallResult{}, ErrBinaryTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return BinaryInstallResult{}, fmt.Errorf("sync Ookla upload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return BinaryInstallResult{}, fmt.Errorf("close Ookla upload: %w", err)
	}
	if err := validateLinuxAMD64ELF(temporaryPath); err != nil {
		return BinaryInstallResult{}, err
	}

	verifyContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	version, err := manager.verifier.Verify(verifyContext, temporaryPath)
	cancel()
	if err != nil {
		return BinaryInstallResult{}, fmt.Errorf("%w: %s", ErrInvalidBinary, providers.SanitizeOutput(err.Error(), 512))
	}
	if err := os.Rename(temporaryPath, manager.path); err != nil {
		return BinaryInstallResult{}, fmt.Errorf("activate Ookla executable: %w", err)
	}
	keep = true
	if err := os.Chmod(manager.path, 0o700); err != nil {
		return BinaryInstallResult{}, fmt.Errorf("secure Ookla executable permissions: %w", err)
	}
	status := manager.Status()
	status.Installed = true
	return BinaryInstallResult{BinaryStatus: status, Version: version, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (manager *BinaryManager) prepareParent() error {
	if info, err := os.Lstat(manager.dataDir); err != nil {
		return fmt.Errorf("inspect APP_DATA_DIR: %w", err)
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("APP_DATA_DIR must be a real directory")
	}
	parent := filepath.Dir(manager.path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create Ookla provider directory: %w", err)
	}
	relative, _ := filepath.Rel(manager.dataDir, parent)
	current := manager.dataDir
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect Ookla provider directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("ookla provider directory must not contain symbolic links")
		}
	}
	return nil
}

func validateLinuxAMD64ELF(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: cannot inspect file", ErrInvalidBinary)
	}
	header := make([]byte, 20)
	_, readErr := io.ReadFull(file, header)
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("%w: incomplete ELF header", ErrInvalidBinary)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: cannot finish inspecting file", ErrInvalidBinary)
	}
	if string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 {
		return fmt.Errorf("%w: expected a 64-bit little-endian ELF file", ErrInvalidBinary)
	}
	fileType := binary.LittleEndian.Uint16(header[16:18])
	machine := binary.LittleEndian.Uint16(header[18:20])
	if (fileType != 2 && fileType != 3) || machine != 62 {
		return fmt.Errorf("%w: expected a Linux amd64 executable", ErrInvalidBinary)
	}
	return nil
}

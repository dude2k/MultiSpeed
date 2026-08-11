// Package process executes provider CLIs with bounded output and group cancellation.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxArgumentLength = 4096

const providerEnvironmentPrefix = "MULTISPEED_PROVIDER_"

type Request struct {
	Binary        string
	Arguments     []string
	Stdin         []byte
	OutputLimit   int
	HomeDirectory string
	// Environment contains narrowly scoped, non-secret execution guardrails.
	// Keys are restricted to the MULTISPEED_PROVIDER_ namespace. Inherited
	// values in that namespace are stripped so only this request can enable a
	// provider-process guardrail.
	Environment map[string]string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type Runner interface {
	Run(context.Context, Request) (Result, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.Binary) == "" || strings.ContainsAny(request.Binary, "\x00\r\n") {
		return Result{}, errors.New("provider executable is invalid")
	}
	if len(request.Arguments) > 128 {
		return Result{}, errors.New("too many provider arguments")
	}
	for _, argument := range request.Arguments {
		if len(argument) > maxArgumentLength || strings.ContainsRune(argument, '\x00') {
			return Result{}, errors.New("provider argument is invalid")
		}
	}
	homeDirectory, err := validateHomeDirectory(request.HomeDirectory)
	if err != nil {
		return Result{}, err
	}
	environment, err := providerEnvironment(request.Environment, homeDirectory)
	if err != nil {
		return Result{}, err
	}
	limit := request.OutputLimit
	if limit < 1024 {
		limit = 256 << 10
	}
	if limit > 1<<20 {
		limit = 1 << 20
	}
	stdout := &limitedBuffer{remaining: limit}
	stderr := &limitedBuffer{remaining: min(limit, 64<<10)}
	command := exec.CommandContext(ctx, request.Binary, request.Arguments...)
	if environment != nil {
		command.Env = environment
	}
	configureProcessGroup(command)
	command.Stdout, command.Stderr = stdout, stderr
	if len(request.Stdin) > 0 {
		command.Stdin = bytes.NewReader(request.Stdin)
	}
	started := time.Now()
	err = command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0, Duration: time.Since(started)}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		result.ExitCode = -1
	}
	if stdout.exceeded || stderr.exceeded {
		return result, fmt.Errorf("provider output exceeded %d byte safety limit", limit)
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("provider process exited with code %d: %w", result.ExitCode, err)
	}
	return result, nil
}

func validateHomeDirectory(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > maxArgumentLength || strings.ContainsAny(value, "\x00\r\n") || !filepath.IsAbs(value) {
		return "", errors.New("provider home directory is invalid")
	}
	return filepath.Clean(value), nil
}

func providerEnvironment(overrides map[string]string, homeDirectory string) ([]string, error) {
	if len(overrides) > 16 {
		return nil, errors.New("too many provider environment values")
	}
	keys := make([]string, 0, len(overrides))
	for key, value := range overrides {
		if !validProviderEnvironmentKey(key) || len(value) > maxArgumentLength || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("provider environment value is invalid")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(os.Environ())+len(keys))
	for _, inherited := range os.Environ() {
		name, _, _ := strings.Cut(inherited, "=")
		upperName := strings.ToUpper(name)
		if !strings.HasPrefix(upperName, providerEnvironmentPrefix) &&
			(homeDirectory == "" || (upperName != "HOME" && upperName != "XDG_CONFIG_HOME")) {
			environment = append(environment, inherited)
		}
	}
	if homeDirectory != "" {
		environment = append(environment, "HOME="+homeDirectory, "XDG_CONFIG_HOME="+filepath.Join(homeDirectory, ".config"))
	}
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment, nil
}

func validProviderEnvironmentKey(key string) bool {
	if !strings.HasPrefix(key, providerEnvironmentPrefix) || len(key) > 128 {
		return false
	}
	for _, character := range key {
		if character != '_' && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if b.remaining <= 0 {
		b.exceeded = true
		return original, nil
	}
	write := data
	if len(write) > b.remaining {
		write = write[:b.remaining]
		b.exceeded = true
	}
	_, _ = b.buffer.Write(write)
	b.remaining -= len(write)
	return original, nil
}
func (b *limitedBuffer) String() string { return b.buffer.String() }

var _ io.Writer = (*limitedBuffer)(nil)

// Package ookla integrates an operator-installed official Ookla Speedtest CLI.
package ookla

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type Adapter struct {
	binary           string
	acceptance       AcceptanceSource
	runner           providerprocess.Runner
	runtimeDirectory string
	runtimeLocksMu   sync.Mutex
	runtimeLocks     map[string]*runtimeLock
}

type runtimeLock struct {
	semaphore  chan struct{}
	references int
}

type AcceptanceSource func(context.Context) (bool, error)

func New(binary string, accepted bool, runner providerprocess.Runner) *Adapter {
	return NewWithAcceptanceSource(binary, func(context.Context) (bool, error) { return accepted, nil }, runner)
}

func NewWithAcceptanceSource(binary string, acceptance AcceptanceSource, runner providerprocess.Runner) *Adapter {
	return NewWithAcceptanceSourceAndRuntimeDirectory(binary, acceptance, "", runner)
}

func NewWithAcceptanceSourceAndRuntimeDirectory(binary string, acceptance AcceptanceSource, runtimeDirectory string, runner providerprocess.Runner) *Adapter {
	if runner == nil {
		runner = providerprocess.ExecRunner{}
	}
	if acceptance == nil {
		acceptance = func(context.Context) (bool, error) { return false, nil }
	}
	return &Adapter{binary: binary, acceptance: acceptance, runner: runner, runtimeDirectory: runtimeDirectory}
}

func (*Adapter) ID() models.ProviderID { return models.ProviderOokla }
func (*Adapter) DisplayName() string   { return "Ookla Speedtest" }
func (*Adapter) Capabilities() providers.Capabilities {
	return providers.Capabilities{ServerDiscovery: true, FixedServerIDs: true, InterfaceBinding: true, SourceAddressBinding: true, IPv4: true, IPv6: true, Jitter: true, PacketLoss: true, ResultURLs: true}
}

func (a *Adapter) Availability(ctx context.Context) providers.Availability {
	accepted, err := a.acceptance(ctx)
	if err != nil {
		return providers.Availability{Message: "Ookla is disabled because the terms acknowledgement could not be verified.", UnavailabilityReason: providers.UnavailabilityReasonPolicy}
	}
	if !accepted {
		return providers.Availability{Message: "Ookla is disabled. Agree to its current EULA and Terms of Use, review its Privacy Policy, authorize the non-interactive CLI acceptance flags, and record the technical acknowledgement in MultiSpeed settings before use.", UnavailabilityReason: providers.UnavailabilityReasonPolicy}
	}
	if _, err := exec.LookPath(a.binary); err != nil {
		return providers.Availability{Message: "The operator-installed Ookla 'speedtest' executable was not found. It is not distributed with MultiSpeed.", UnavailabilityReason: providers.UnavailabilityReasonRuntime}
	}
	version, err := a.Version(ctx)
	if err != nil {
		return providers.Availability{Message: "The Ookla executable was found but could not be queried: " + providers.SanitizeOutput(err.Error(), 512), UnavailabilityReason: providers.UnavailabilityReasonRuntime}
	}
	return providers.Availability{Available: true, Version: version, Message: "Operator-installed CLI; use is governed by Ookla's terms."}
}

func (a *Adapter) Version(ctx context.Context) (string, error) {
	request, err := a.processRequest([]string{"--version"}, 16<<10, "", "")
	if err != nil {
		return "", err
	}
	result, err := a.runProcess(ctx, request)
	if err != nil {
		return "", err
	}
	version := providers.SanitizeOutput(result.Stdout, 512)
	if version == "" {
		return "", errors.New("ookla CLI returned an empty version")
	}
	return version, nil
}

func (a *Adapter) Validate(_ context.Context, target providers.TestTarget) error {
	switch target.SelectionMode {
	case "", "automatic":
		return nil
	case "fixed":
		if !numericID(target.ServerID) {
			return fmt.Errorf("ookla fixed server ID must contain only digits: %w", providers.ErrInvalidTarget)
		}
		return nil
	default:
		return fmt.Errorf("ookla supports automatic or fixed target selection: %w", providers.ErrInvalidTarget)
	}
}

func (a *Adapter) ListServers(ctx context.Context, request providers.ServerListRequest) ([]providers.Server, error) {
	if availability := a.Availability(ctx); !availability.Available {
		return nil, fmt.Errorf("%s: %w", availability.Message, providers.ErrUnavailable)
	}
	args := a.baseArgs(request.InterfaceName, request.SourceIP)
	args = append(args, "--servers")
	processRequest, err := a.processRequest(args, providers.MaxStoredOutput, request.InterfaceName, request.SourceIP)
	if err != nil {
		return nil, err
	}
	result, err := a.runProcess(ctx, processRequest)
	if err != nil {
		return nil, fmt.Errorf("ookla server discovery failed: %s: %w", failureMessage(result.Stderr), err)
	}
	servers, err := parseServerList([]byte(result.Stdout))
	if err != nil {
		return nil, err
	}
	return filterServers(servers, request.Search, request.Limit), nil
}

func (a *Adapter) Run(ctx context.Context, request providers.RunRequest) (providers.ProviderResult, error) {
	if availability := a.Availability(ctx); !availability.Available {
		return providers.ProviderResult{}, fmt.Errorf("%s: %w", availability.Message, providers.ErrUnavailable)
	}
	if err := a.Validate(ctx, request.Target); err != nil {
		return providers.ProviderResult{}, err
	}
	args := a.baseArgs(request.InterfaceName, request.SourceIP)
	if request.Target.SelectionMode == "fixed" {
		args = append(args, "--server-id="+request.Target.ServerID)
	}
	processRequest, err := a.processRequest(args, providers.MaxStoredOutput, request.InterfaceName, request.SourceIP)
	if err != nil {
		return providers.ProviderResult{}, err
	}
	processResult, err := a.runProcess(ctx, processRequest)
	if err != nil {
		return providers.ProviderResult{ExitCode: &processResult.ExitCode, DurationMilliseconds: processResult.Duration.Milliseconds(), RawResponse: providers.SanitizeOutput(processResult.Stdout, providers.MaxStoredOutput)},
			fmt.Errorf("ookla test failed (exit %d): %s: %w", processResult.ExitCode, failureMessage(processResult.Stderr), err)
	}
	parsed, err := parseResult([]byte(processResult.Stdout))
	if err != nil {
		return providers.ProviderResult{ExitCode: &processResult.ExitCode, RawResponse: providers.SanitizeOutput(processResult.Stdout, providers.MaxStoredOutput)}, err
	}
	parsed.ExitCode = &processResult.ExitCode
	parsed.DurationMilliseconds = processResult.Duration.Milliseconds()
	parsed.RawResponse = providers.SanitizeOutput(processResult.Stdout, providers.MaxStoredOutput)
	parsed.ProviderVersion, _ = a.Version(ctx)
	return parsed, nil
}

func failureMessage(stderr string) string {
	summary := failureSummary(stderr)
	lowerSummary := strings.ToLower(summary)
	hasTimeout := strings.Contains(lowerSummary, "timeout occurred in connect")
	hasStateFailure := strings.Contains(lowerSummary, "failed to save settings") ||
		(strings.Contains(lowerSummary, "permission denied") &&
			(strings.Contains(lowerSummary, ".config/ookla") || strings.Contains(lowerSummary, "speedtest-cli.json") || strings.Contains(lowerSummary, "path used")))
	switch {
	case hasStateFailure && hasTimeout:
		return "the CLI could not persist settings in its managed runtime directory; verify APP_DATA_DIR is writable by the effective container user (image default UID 10001). After fixing storage, also verify this WAN's gateway, DNS, firewall, and target-server reachability if the connection timeout remains. Diagnostic: " + summary
	case hasStateFailure:
		return "the CLI could not persist settings in its managed runtime directory; verify APP_DATA_DIR is writable by the effective container user (image default UID 10001). Diagnostic: " + summary
	case hasTimeout:
		return "the CLI could not connect through the selected source path; verify this WAN's gateway, DNS, firewall, and target-server reachability. Diagnostic: " + summary
	default:
		return summary
	}
}

func failureSummary(stderr string) string {
	type logLine struct {
		Message string `json:"message"`
	}
	messages := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var parsed logLine
		if json.Unmarshal([]byte(line), &parsed) != nil {
			continue
		}
		message := providers.SanitizeOutput(parsed.Message, 512)
		if message == "" {
			continue
		}
		if _, exists := seen[message]; exists {
			continue
		}
		seen[message] = struct{}{}
		messages = append(messages, message)
		if len(messages) == 4 {
			break
		}
	}
	if len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	if fallback := providers.SanitizeOutput(stderr, 2048); fallback != "" {
		return fallback
	}
	return "the CLI returned no diagnostic output"
}

func (a *Adapter) processRequest(arguments []string, outputLimit int, interfaceName, sourceIP string) (providerprocess.Request, error) {
	homeDirectory, err := a.prepareRuntimeHome(interfaceName, sourceIP)
	if err != nil {
		return providerprocess.Request{}, fmt.Errorf("prepare Ookla CLI state under APP_DATA_DIR (must be writable by the effective container user; image default UID 10001): %w", err)
	}
	return providerprocess.Request{
		Binary:        a.binary,
		Arguments:     arguments,
		OutputLimit:   outputLimit,
		HomeDirectory: homeDirectory,
	}, nil
}

func (a *Adapter) runProcess(ctx context.Context, request providerprocess.Request) (providerprocess.Result, error) {
	release, err := a.acquireRuntimeLock(ctx, request.HomeDirectory)
	if err != nil {
		return providerprocess.Result{ExitCode: -1}, err
	}
	defer release()
	return a.runner.Run(ctx, request)
}

func (a *Adapter) acquireRuntimeLock(ctx context.Context, homeDirectory string) (func(), error) {
	a.runtimeLocksMu.Lock()
	if a.runtimeLocks == nil {
		a.runtimeLocks = make(map[string]*runtimeLock)
	}
	lock, exists := a.runtimeLocks[homeDirectory]
	if !exists {
		lock = &runtimeLock{semaphore: make(chan struct{}, 1)}
		a.runtimeLocks[homeDirectory] = lock
	}
	lock.references++
	a.runtimeLocksMu.Unlock()

	select {
	case lock.semaphore <- struct{}{}:
		return func() {
			<-lock.semaphore
			a.releaseRuntimeLockReference(homeDirectory, lock)
		}, nil
	case <-ctx.Done():
		a.releaseRuntimeLockReference(homeDirectory, lock)
		return nil, ctx.Err()
	}
}

func (a *Adapter) releaseRuntimeLockReference(homeDirectory string, lock *runtimeLock) {
	a.runtimeLocksMu.Lock()
	defer a.runtimeLocksMu.Unlock()
	lock.references--
	if lock.references == 0 && a.runtimeLocks[homeDirectory] == lock {
		delete(a.runtimeLocks, homeDirectory)
	}
}

func (a *Adapter) prepareRuntimeHome(interfaceName, sourceIP string) (string, error) {
	if a.runtimeDirectory == "" {
		return "", nil
	}
	runtimeDirectory := filepath.Clean(a.runtimeDirectory)
	if !filepath.IsAbs(runtimeDirectory) {
		return "", errors.New("runtime directory must be absolute")
	}
	if err := ensurePrivateDirectory(runtimeDirectory); err != nil {
		return "", fmt.Errorf("prepare runtime root: %w", err)
	}
	pathKey := "default"
	interfaceName = strings.TrimSpace(interfaceName)
	sourceIP = strings.TrimSpace(sourceIP)
	if parsed := net.ParseIP(sourceIP); parsed != nil {
		sourceIP = parsed.String()
	}
	if interfaceName != "" || sourceIP != "" {
		digest := sha256.Sum256([]byte(interfaceName + "\x00" + sourceIP))
		pathKey = fmt.Sprintf("%x", digest[:16])
	}
	homeDirectory := filepath.Join(runtimeDirectory, pathKey)
	if err := ensurePrivateDirectory(homeDirectory); err != nil {
		return "", fmt.Errorf("prepare per-path runtime home: %w", err)
	}
	return homeDirectory, nil
}

func ensurePrivateDirectory(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return errors.New("directory must be absolute")
	}
	root := filepath.VolumeName(path) + string(filepath.Separator)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("directory is outside its filesystem root")
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create directory component: %w", err)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect directory component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("runtime directory path must contain only real directories")
		}
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure runtime directory: %w", err)
	}
	return nil
}

func (a *Adapter) baseArgs(interfaceName, sourceIP string) []string {
	args := []string{"--accept-license", "--accept-gdpr", "--format=json", "--progress=no"}
	// Ookla CLI 1.2.0.84 rejects --interface and --ip when they are used
	// together. Prefer the concrete source address after MultiSpeed has
	// verified that it belongs to the selected interface; fall back to the
	// interface only for callers that do not supply a source address.
	if sourceIP != "" {
		args = append(args, "--ip="+sourceIP)
	} else if interfaceName != "" {
		args = append(args, "--interface="+interfaceName)
	}
	return args
}

func numericID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

type ooklaJSON struct {
	Type string `json:"type"`
	Ping struct {
		Jitter  *float64 `json:"jitter"`
		Latency *float64 `json:"latency"`
	} `json:"ping"`
	Download struct {
		Bandwidth *int64 `json:"bandwidth"`
		Bytes     *int64 `json:"bytes"`
	} `json:"download"`
	Upload struct {
		Bandwidth *int64 `json:"bandwidth"`
		Bytes     *int64 `json:"bytes"`
	} `json:"upload"`
	PacketLoss *float64 `json:"packetLoss"`
	Interface  struct {
		ExternalIP string `json:"externalIp"`
	} `json:"interface"`
	Server struct {
		ID                                int64 `json:"id"`
		Host, Name, Location, Country, IP string
		Sponsor                           string `json:"sponsor"`
	} `json:"server"`
	Result struct {
		URL string `json:"url"`
	} `json:"result"`
}

func parseResult(data []byte) (providers.ProviderResult, error) {
	var value ooklaJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse ookla JSON: %w", err)
	}
	if value.Type != "result" || value.Download.Bandwidth == nil || value.Upload.Bandwidth == nil || value.Ping.Latency == nil ||
		value.Ping.Jitter == nil || value.Download.Bytes == nil || value.Upload.Bytes == nil {
		return providers.ProviderResult{}, errors.New("ookla JSON did not contain a complete result")
	}
	if *value.Download.Bandwidth < 0 || *value.Download.Bandwidth > math.MaxInt64/8 || *value.Upload.Bandwidth < 0 || *value.Upload.Bandwidth > math.MaxInt64/8 ||
		*value.Download.Bytes < 0 || *value.Upload.Bytes < 0 || *value.Ping.Latency < 0 || *value.Ping.Jitter < 0 ||
		math.IsNaN(*value.Ping.Latency) || math.IsInf(*value.Ping.Latency, 0) || math.IsNaN(*value.Ping.Jitter) || math.IsInf(*value.Ping.Jitter, 0) {
		return providers.ProviderResult{}, errors.New("ookla JSON contained invalid metrics")
	}
	if value.PacketLoss != nil && (*value.PacketLoss < 0 || *value.PacketLoss > 100 || math.IsNaN(*value.PacketLoss) || math.IsInf(*value.PacketLoss, 0)) {
		return providers.ProviderResult{}, errors.New("ookla JSON contained invalid packet loss")
	}
	down, up := *value.Download.Bandwidth*8, *value.Upload.Bandwidth*8
	latency, jitter := *value.Ping.Latency, *value.Ping.Jitter
	downBytes, upBytes := *value.Download.Bytes, *value.Upload.Bytes
	return providers.ProviderResult{DownloadBitsPerSecond: &down, UploadBitsPerSecond: &up, LatencyMilliseconds: &latency,
		JitterMilliseconds: &jitter, PacketLossPercent: value.PacketLoss, DownloadBytes: &downBytes, UploadBytes: &upBytes,
		PublicIP: value.Interface.ExternalIP, Server: providers.Server{ID: strconv.FormatInt(value.Server.ID, 10), Host: value.Server.Host,
			Name: value.Server.Name, Sponsor: value.Server.Sponsor, Location: value.Server.Location, Country: value.Server.Country}, ResultURL: value.Result.URL}, nil
}

func parseServerList(data []byte) ([]providers.Server, error) {
	var wrapper struct {
		Servers []struct {
			ID                                     json.RawMessage `json:"id"`
			Host, Name, Sponsor, Location, Country string
			Distance                               float64 `json:"distance"`
		} `json:"servers"`
	}
	if json.Unmarshal(data, &wrapper) == nil && len(wrapper.Servers) > 0 {
		items := make([]providers.Server, 0, len(wrapper.Servers))
		for _, value := range wrapper.Servers {
			id := strings.Trim(string(value.ID), `"`)
			items = append(items, providers.Server{ID: id, Host: value.Host, Name: value.Name, Sponsor: value.Sponsor, Location: value.Location, Country: value.Country, Distance: value.Distance})
		}
		return items, nil
	}
	items := make([]providers.Server, 0)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !numericID(fields[0]) {
			continue
		}
		items = append(items, providers.Server{ID: fields[0], Name: strings.Join(fields[1:], " ")})
	}
	if len(items) == 0 {
		return nil, errors.New("ookla CLI returned no parseable servers")
	}
	return items, nil
}

func filterServers(items []providers.Server, search string, limit int) []providers.Server {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	result := make([]providers.Server, 0, min(limit, len(items)))
	for _, item := range items {
		haystack := strings.ToLower(item.ID + " " + item.Name + " " + item.Sponsor + " " + item.Location + " " + item.Country + " " + item.Host)
		if needle != "" && !strings.Contains(haystack, needle) {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

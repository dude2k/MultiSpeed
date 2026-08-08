// Package ookla integrates an operator-installed official Ookla Speedtest CLI.
package ookla

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type Adapter struct {
	binary     string
	acceptance AcceptanceSource
	runner     providerprocess.Runner
}

type AcceptanceSource func(context.Context) (bool, error)

func New(binary string, accepted bool, runner providerprocess.Runner) *Adapter {
	return NewWithAcceptanceSource(binary, func(context.Context) (bool, error) { return accepted, nil }, runner)
}

func NewWithAcceptanceSource(binary string, acceptance AcceptanceSource, runner providerprocess.Runner) *Adapter {
	if runner == nil {
		runner = providerprocess.ExecRunner{}
	}
	if acceptance == nil {
		acceptance = func(context.Context) (bool, error) { return false, nil }
	}
	return &Adapter{binary: binary, acceptance: acceptance, runner: runner}
}

func (*Adapter) ID() models.ProviderID { return models.ProviderOokla }
func (*Adapter) DisplayName() string   { return "Ookla Speedtest" }
func (*Adapter) Capabilities() providers.Capabilities {
	return providers.Capabilities{ServerDiscovery: true, FixedServerIDs: true, InterfaceBinding: true, SourceAddressBinding: true, IPv4: true, IPv6: true, Jitter: true, PacketLoss: true, ResultURLs: true}
}

func (a *Adapter) Availability(ctx context.Context) providers.Availability {
	accepted, err := a.acceptance(ctx)
	if err != nil {
		return providers.Availability{Message: "Ookla is disabled because EULA acceptance could not be verified."}
	}
	if !accepted {
		return providers.Availability{Message: "Ookla is disabled. Review its current terms and explicitly record EULA acceptance in MultiSpeed settings before use."}
	}
	if _, err := exec.LookPath(a.binary); err != nil {
		return providers.Availability{Message: "The operator-installed Ookla 'speedtest' executable was not found. It is not distributed with MultiSpeed."}
	}
	version, err := a.Version(ctx)
	if err != nil {
		return providers.Availability{Message: "The Ookla executable was found but could not be queried: " + providers.SanitizeOutput(err.Error(), 512)}
	}
	return providers.Availability{Available: true, Version: version, Message: "Operator-installed CLI; use is governed by Ookla's terms."}
}

func (a *Adapter) Version(ctx context.Context) (string, error) {
	result, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: []string{"--version"}, OutputLimit: 16 << 10})
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
	result, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: args, OutputLimit: providers.MaxStoredOutput})
	if err != nil {
		return nil, fmt.Errorf("ookla server discovery failed: %s: %w", providers.SanitizeOutput(result.Stderr, 2048), err)
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
	processResult, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: args, OutputLimit: providers.MaxStoredOutput})
	if err != nil {
		return providers.ProviderResult{ExitCode: &processResult.ExitCode, DurationMilliseconds: processResult.Duration.Milliseconds(), RawResponse: providers.SanitizeOutput(processResult.Stdout, providers.MaxStoredOutput)},
			fmt.Errorf("ookla test failed (exit %d): %s: %w", processResult.ExitCode, providers.SanitizeOutput(processResult.Stderr, 4096), err)
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

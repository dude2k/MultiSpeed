// Package librespeed integrates the official pinned LibreSpeed CLI.
package librespeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type Adapter struct {
	binary                 string
	runner                 providerprocess.Runner
	customServers          providers.CustomServerURLPolicy
	resolveCustomServerIPs func(context.Context, string, string, string) ([]string, error)
}

const (
	requiredDNSPatchMarker            = "+multispeed.dns2.xnet055"
	allowedServerEndpointsEnvironment = "MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS"
)

func New(binary string, runner providerprocess.Runner) *Adapter {
	policy, err := providers.NewCustomServerURLPolicy(nil)
	if err != nil {
		panic(err)
	}
	return NewWithCustomServerURLPolicy(binary, runner, policy)
}

func NewWithCustomServerURLPolicy(binary string, runner providerprocess.Runner, policy providers.CustomServerURLPolicy) *Adapter {
	if runner == nil {
		runner = providerprocess.ExecRunner{}
	}
	return &Adapter{binary: binary, runner: runner, customServers: policy, resolveCustomServerIPs: resolveCustomServerIPs}
}
func (*Adapter) ID() models.ProviderID { return models.ProviderLibreSpeed }
func (*Adapter) DisplayName() string   { return "LibreSpeed" }
func (a *Adapter) Capabilities() providers.Capabilities {
	return providers.Capabilities{ServerDiscovery: true, FixedServerIDs: true, CustomServerURLs: a.customServers.Enabled(), InterfaceBinding: false, SourceAddressBinding: true, IPv4: true, IPv6: true, Jitter: true, ResultURLs: true}
}

func (a *Adapter) Availability(ctx context.Context) providers.Availability {
	if _, err := exec.LookPath(a.binary); err != nil {
		return providers.Availability{Message: "The LibreSpeed CLI executable was not found."}
	}
	version, err := a.Version(ctx)
	if err != nil {
		return providers.Availability{Message: "LibreSpeed CLI could not be queried: " + providers.SanitizeOutput(err.Error(), 512)}
	}
	if !strings.Contains(version, requiredDNSPatchMarker) {
		return providers.Availability{Version: version, Message: "LibreSpeed CLI lacks MultiSpeed's required source-bound DNS, destination pinning, or patched dependency marker; use the bundled executable."}
	}
	message := "Telemetry is disabled by default."
	if !a.customServers.Enabled() {
		message += " Custom backend URLs are disabled until APP_ALLOWED_CUSTOM_SERVER_URLS is configured."
	}
	return providers.Availability{Available: true, Version: version, Message: message}
}

func (a *Adapter) Version(ctx context.Context) (string, error) {
	result, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: []string{"--version"}, OutputLimit: 16 << 10})
	if err != nil {
		return "", err
	}
	value := providers.SanitizeOutput(result.Stdout, 512)
	if value == "" {
		return "", errors.New("libreSpeed CLI returned an empty version")
	}
	return value, nil
}

func (a *Adapter) Validate(_ context.Context, target providers.TestTarget) error {
	switch target.SelectionMode {
	case "", "automatic":
		return nil
	case "fixed":
		if !numericID(target.ServerID) {
			return fmt.Errorf("libreSpeed server ID must contain only digits: %w", providers.ErrInvalidTarget)
		}
		return nil
	case "custom":
		if err := providers.ValidateHTTPURL(target.ServerURL, optionBool(target.CustomServerDefinition, "allowInsecure")); err != nil {
			return err
		}
		if _, err := a.customServers.Authorize(target.ServerURL, optionBool(target.CustomServerDefinition, "allowInsecure")); err != nil {
			return err
		}
		_, err := customDefinition(target)
		return err
	default:
		return fmt.Errorf("libreSpeed target mode must be automatic, fixed, or custom: %w", providers.ErrInvalidTarget)
	}
}

func (a *Adapter) ListServers(ctx context.Context, request providers.ServerListRequest) ([]providers.Server, error) {
	if availability := a.Availability(ctx); !availability.Available {
		return nil, fmt.Errorf("%s: %w", availability.Message, providers.ErrUnavailable)
	}
	args := baseArgs(request.SourceIP, request.IPFamily, 30, nil)
	args = append(args, "--list")
	result, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: args, OutputLimit: providers.MaxStoredOutput})
	if err != nil {
		return nil, fmt.Errorf("libreSpeed server discovery failed: %s: %w", providers.SanitizeOutput(result.Stderr, 2048), err)
	}
	servers, err := parseServers([]byte(result.Stdout))
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
	args := baseArgs(request.SourceIP, request.IPFamily, request.TimeoutSeconds, request.Options)
	var stdin []byte
	switch request.Target.SelectionMode {
	case "fixed":
		args = append(args, "--server", request.Target.ServerID)
	case "custom":
		authorizedURL, err := a.customServers.Authorize(request.Target.ServerURL, optionBool(request.Target.CustomServerDefinition, "allowInsecure"))
		if err != nil {
			return providers.ProviderResult{}, err
		}
		request.Target.ServerURL = authorizedURL
		definition, err := customDefinition(request.Target)
		if err != nil {
			return providers.ProviderResult{}, err
		}
		resolver := a.resolveCustomServerIPs
		if resolver == nil {
			return providers.ProviderResult{}, errors.New("custom LibreSpeed destination resolver is unavailable")
		}
		allowedIPs, err := resolver(ctx, authorizedURL, request.SourceIP, request.IPFamily)
		if err != nil {
			return providers.ProviderResult{}, fmt.Errorf("authorize custom LibreSpeed destination: %w", err)
		}
		allowedEndpoints, err := customServerEndpoints(authorizedURL, allowedIPs)
		if err != nil {
			return providers.ProviderResult{}, fmt.Errorf("pin custom LibreSpeed destination: %w", err)
		}
		stdin, err = json.Marshal([]map[string]any{definition})
		if err != nil {
			return providers.ProviderResult{}, err
		}
		args = append(args, "--local-json", "-", "--server", "1")
		requestEnvironment := strings.Join(allowedEndpoints, ",")
		result, runErr := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: args, Stdin: stdin, OutputLimit: providers.MaxStoredOutput,
			Environment: map[string]string{allowedServerEndpointsEnvironment: requestEnvironment}})
		return a.finishRun(ctx, request, result, runErr)
	}
	result, err := a.runner.Run(ctx, providerprocess.Request{Binary: a.binary, Arguments: args, Stdin: stdin, OutputLimit: providers.MaxStoredOutput})
	return a.finishRun(ctx, request, result, err)
}

func (a *Adapter) finishRun(ctx context.Context, request providers.RunRequest, result providerprocess.Result, err error) (providers.ProviderResult, error) {
	if err != nil {
		return providers.ProviderResult{ExitCode: &result.ExitCode, DurationMilliseconds: result.Duration.Milliseconds(), RawResponse: providers.SanitizeOutput(result.Stdout, providers.MaxStoredOutput)},
			fmt.Errorf("libreSpeed test failed (exit %d): %s: %w", result.ExitCode, providers.SanitizeOutput(result.Stderr, 4096), err)
	}
	parsed, err := parseResult([]byte(result.Stdout))
	if err != nil {
		return providers.ProviderResult{ExitCode: &result.ExitCode, RawResponse: providers.SanitizeOutput(result.Stdout, providers.MaxStoredOutput)}, err
	}
	parsed.ExitCode = &result.ExitCode
	parsed.DurationMilliseconds = result.Duration.Milliseconds()
	parsed.RawResponse = providers.SanitizeOutput(result.Stdout, providers.MaxStoredOutput)
	parsed.ProviderVersion = models.LibreSpeedBitsMethodologyVersion
	if cliVersion, versionErr := a.Version(ctx); versionErr == nil {
		parsed.ProviderVersion = cliVersion + "; " + models.LibreSpeedBitsMethodologyVersion
	}
	parsed.TLSVerificationDisabled = optionBool(request.Options, "skipTlsVerification")
	if request.Target.SelectionMode == "fixed" {
		parsed.Server.ID = request.Target.ServerID
	}
	return parsed, nil
}

func resolveCustomServerIPs(ctx context.Context, rawURL, sourceIP, ipFamily string) ([]string, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("custom LibreSpeed server URL is invalid")
	}
	source := net.ParseIP(sourceIP)
	if source == nil || source.IsUnspecified() || source.IsMulticast() {
		return nil, errors.New("custom LibreSpeed source IP must be a concrete unicast address")
	}
	wantIPv4 := source.To4() != nil
	if ipFamily == "ipv4" && !wantIPv4 || ipFamily == "ipv6" && wantIPv4 {
		return nil, errors.New("custom LibreSpeed source IP does not match the selected IP family")
	}
	if ipFamily != "auto" && ipFamily != "ipv4" && ipFamily != "ipv6" {
		return nil, errors.New("custom LibreSpeed IP family is invalid")
	}
	addresses := make([]net.IPAddr, 0, 1)
	if literal := net.ParseIP(parsed.Hostname()); literal != nil {
		addresses = append(addresses, net.IPAddr{IP: literal})
	} else {
		resolver, resolverErr := network.NewSourceBoundResolver(source)
		if resolverErr != nil {
			return nil, fmt.Errorf("create source-bound resolver: %w", resolverErr)
		}
		addresses, err = resolver.LookupIPAddr(ctx, parsed.Hostname())
		if err != nil {
			return nil, fmt.Errorf("source-bound custom server lookup failed: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if address.Zone != "" || address.IP == nil || address.IP.IsUnspecified() || address.IP.IsMulticast() {
			return nil, errors.New("custom LibreSpeed server resolved to an invalid IP address")
		}
		if (address.IP.To4() != nil) != wantIPv4 {
			continue
		}
		canonical := address.IP.String()
		seen[canonical] = struct{}{}
		if len(seen) > 64 {
			return nil, errors.New("custom LibreSpeed server resolved to too many IP addresses")
		}
	}
	result := make([]string, 0, len(seen))
	for address := range seen {
		result = append(result, address)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, errors.New("custom LibreSpeed server has no address matching the selected source family")
	}
	return result, nil
}

func customServerEndpoints(rawURL string, addresses []string) ([]string, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("custom LibreSpeed server URL is invalid")
	}
	portText := parsed.Port()
	if portText == "" {
		switch parsed.Scheme {
		case "http":
			portText = "80"
		case "https":
			portText = "443"
		default:
			return nil, errors.New("custom LibreSpeed server URL has an invalid scheme")
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("custom LibreSpeed server URL has an invalid port")
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, errors.New("custom LibreSpeed destination has an invalid address count")
	}
	seen := make(map[netip.AddrPort]struct{}, len(addresses))
	for _, rawAddress := range addresses {
		address, parseErr := netip.ParseAddr(rawAddress)
		if parseErr != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
			return nil, errors.New("custom LibreSpeed destination contains an invalid IP address")
		}
		seen[netip.AddrPortFrom(address.Unmap(), uint16(port))] = struct{}{}
	}
	endpoints := make([]string, 0, len(seen))
	for endpoint := range seen {
		endpoints = append(endpoints, endpoint.String())
	}
	sort.Strings(endpoints)
	return endpoints, nil
}

func baseArgs(sourceIP, family string, timeout int, options map[string]any) []string {
	if timeout < 5 {
		timeout = 120
	}
	args := []string{"--json", "--source", sourceIP, "--no-icmp", "--timeout", strconv.Itoa(timeout), "--telemetry-level", "disabled"}
	if family == "ipv4" {
		args = append(args, "--ipv4")
	}
	if family == "ipv6" {
		args = append(args, "--ipv6")
	}
	if optionBool(options, "skipTlsVerification") {
		args = append(args, "--skip-cert-verify")
	}
	return args
}

func customDefinition(target providers.TestTarget) (map[string]any, error) {
	base := strings.TrimRight(target.ServerURL, "/")
	definition := map[string]any{"id": 1, "name": "Custom LibreSpeed", "server": base, "dlURL": "garbage.php", "ulURL": "empty.php", "pingURL": "empty.php", "getIpURL": "getIP.php"}
	for key, rawValue := range target.CustomServerDefinition {
		switch key {
		case "allowInsecure":
			if _, ok := rawValue.(bool); !ok {
				return nil, errors.New("custom LibreSpeed allowInsecure must be a boolean")
			}
		case "name", "dlURL", "ulURL", "pingURL", "getIpURL":
			value, ok := rawValue.(string)
			if !ok || value == "" || len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n") {
				return nil, fmt.Errorf("custom LibreSpeed %s is invalid", key)
			}
			if key != "name" {
				if !validCustomEndpoint(value) {
					return nil, fmt.Errorf("custom LibreSpeed %s must be a relative endpoint", key)
				}
			}
			definition[key] = value
		default:
			return nil, fmt.Errorf("unsupported custom LibreSpeed field %q", key)
		}
	}
	return definition, nil
}

func validCustomEndpoint(value string) bool {
	lower := strings.ToLower(value)
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") ||
		strings.Contains(lower, "%00") || strings.Contains(lower, "%0a") || strings.Contains(lower, "%0d") {
		return false
	}
	endpoint, err := url.ParseRequestURI(value)
	if err != nil || endpoint.IsAbs() || endpoint.Host != "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.Opaque != "" || endpoint.Path == "" {
		return false
	}
	for _, segment := range strings.Split(endpoint.Path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func optionBool(options map[string]any, key string) bool {
	value, _ := options[key].(bool)
	return value
}

type libreResult struct {
	Timestamp string `json:"timestamp"`
	Server    struct {
		ID        json.RawMessage `json:"id"`
		Name, URL string
	} `json:"server"`
	Client struct {
		IP string `json:"ip"`
	} `json:"client"`
	BytesSent     int64           `json:"bytes_sent"`
	BytesReceived int64           `json:"bytes_received"`
	Ping          json.RawMessage `json:"ping"`
	Jitter        json.RawMessage `json:"jitter"`
	Upload        json.RawMessage `json:"upload"`
	Download      json.RawMessage `json:"download"`
	Share         string          `json:"share"`
}

func parseResult(data []byte) (providers.ProviderResult, error) {
	var values []libreResult
	if err := json.Unmarshal(data, &values); err != nil {
		// Older development builds emitted a single object. Accept it so an
		// operator-provided compatible binary fails gracefully across upgrades.
		var value libreResult
		if objectErr := json.Unmarshal(data, &value); objectErr != nil {
			return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed JSON: %w", err)
		}
		values = []libreResult{value}
	}
	if len(values) != 1 {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed JSON: expected one result, received %d", len(values))
	}
	value := values[0]
	download, err := number(value.Download)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed download: %w", err)
	}
	upload, err := number(value.Upload)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed upload: %w", err)
	}
	ping, err := number(value.Ping)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed latency: %w", err)
	}
	if ping < 0 {
		return providers.ProviderResult{}, errors.New("parse LibreSpeed latency: value is negative")
	}
	jitter, jitterErr := number(value.Jitter)
	if jitterErr == nil && jitter < 0 {
		return providers.ProviderResult{}, errors.New("parse LibreSpeed jitter: value is negative")
	}
	downBPS, err := normalizedBitsPerSecond(download)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed download: %w", err)
	}
	upBPS, err := normalizedBitsPerSecond(upload)
	if err != nil {
		return providers.ProviderResult{}, fmt.Errorf("parse LibreSpeed upload: %w", err)
	}
	downBytes, upBytes := value.BytesReceived, value.BytesSent
	serverHost := ""
	if serverURL, parseErr := url.Parse(value.Server.URL); parseErr == nil {
		serverHost = serverURL.Host
	}
	result := providers.ProviderResult{DownloadBitsPerSecond: &downBPS, UploadBitsPerSecond: &upBPS, LatencyMilliseconds: &ping,
		DownloadBytes: &downBytes, UploadBytes: &upBytes, PublicIP: value.Client.IP, ResultURL: value.Share,
		Server: providers.Server{ID: strings.Trim(string(value.Server.ID), `"`), Name: value.Server.Name, Host: serverHost, URL: value.Server.URL}}
	if jitterErr == nil {
		result.JitterMilliseconds = &jitter
	}
	return result, nil
}

func normalizedBitsPerSecond(value float64) (int64, error) {
	// LibreSpeed CLI v1.0.13 emits download and upload JSON values in
	// megabits per second even though its help text describes JSON as bit/s.
	// Normalize the observed wire format into MultiSpeed's canonical bit/s.
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || value > float64(math.MaxInt64)/1_000_000 {
		return 0, errors.New("value is outside the supported range")
	}
	return int64(math.Round(value * 1_000_000)), nil
}

func number(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, errors.New("missing value")
	}
	var value float64
	if json.Unmarshal(raw, &value) == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("value is not a finite number")
	}
	return value, nil
}

func parseServers(data []byte) ([]providers.Server, error) {
	var values []struct {
		ID                         json.RawMessage `json:"id"`
		Name, Server, URL          string
		Sponsor, Location, Country string
		Distance                   float64 `json:"distance"`
	}
	if err := json.Unmarshal(data, &values); err != nil {
		var wrapper struct {
			Servers json.RawMessage `json:"servers"`
		}
		if wrapperErr := json.Unmarshal(data, &wrapper); wrapperErr != nil || json.Unmarshal(wrapper.Servers, &values) != nil {
			return parseServerLines(string(data))
		}
	}
	items := make([]providers.Server, 0, len(values))
	for _, value := range values {
		items = append(items, providers.Server{ID: strings.Trim(string(value.ID), `"`), Name: value.Name, Host: value.Server, URL: value.URL, Sponsor: value.Sponsor, Location: value.Location, Country: value.Country, Distance: value.Distance})
	}
	return items, nil
}

func parseServerLines(output string) ([]providers.Server, error) {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	items := make([]providers.Server, 0, len(lines))
	for lineNumber, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 1 || !numericID(strings.TrimSpace(line[:colon])) {
			return nil, fmt.Errorf("parse LibreSpeed server list line %d: invalid server ID", lineNumber+1)
		}
		id := strings.TrimSpace(line[:colon])
		remainder := strings.TrimSpace(line[colon+1:])
		sponsor := ""
		const sponsorMarker = "  [Sponsor: "
		if marker := strings.LastIndex(remainder, sponsorMarker); marker >= 0 && strings.HasSuffix(remainder, "]") {
			sponsor = strings.TrimSuffix(remainder[marker+len(sponsorMarker):], "]")
			remainder = strings.TrimSpace(remainder[:marker])
		}
		urlStart := strings.LastIndex(remainder, " (")
		if urlStart < 1 || !strings.HasSuffix(remainder, ")") {
			return nil, fmt.Errorf("parse LibreSpeed server list line %d: missing server URL", lineNumber+1)
		}
		name := strings.TrimSpace(remainder[:urlStart])
		serverURL := strings.TrimSpace(remainder[urlStart+2 : len(remainder)-1])
		parsedURL, err := url.ParseRequestURI(serverURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
			return nil, fmt.Errorf("parse LibreSpeed server list line %d: invalid server URL", lineNumber+1)
		}
		if at := strings.Index(sponsor, " @ "); at >= 0 {
			sponsor = strings.TrimSpace(sponsor[:at])
		}
		items = append(items, providers.Server{ID: id, Name: name, Host: parsedURL.Host, URL: serverURL, Sponsor: sponsor, Location: name})
	}
	if len(items) == 0 {
		return nil, errors.New("parse LibreSpeed server list: no servers returned")
	}
	return items, nil
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
func filterServers(items []providers.Server, search string, limit int) []providers.Server {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	result := make([]providers.Server, 0, min(limit, len(items)))
	for _, item := range items {
		if needle != "" && !strings.Contains(strings.ToLower(item.ID+" "+item.Name+" "+item.Host+" "+item.Location+" "+item.Country), needle) {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

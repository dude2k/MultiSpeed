// Package providers defines the common speed-test provider abstraction.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

const (
	MaxStoredOutput         = 256 << 10
	maxCustomServerURLBytes = 2048
)

type Capabilities struct {
	ServerDiscovery      bool `json:"serverDiscovery"`
	FixedServerIDs       bool `json:"fixedServerIds"`
	CustomServerURLs     bool `json:"customServerUrls"`
	InterfaceBinding     bool `json:"interfaceBinding"`
	SourceAddressBinding bool `json:"sourceAddressBinding"`
	IPv4                 bool `json:"ipv4"`
	IPv6                 bool `json:"ipv6"`
	Jitter               bool `json:"jitter"`
	PacketLoss           bool `json:"packetLoss"`
	ResultURLs           bool `json:"resultUrls"`
}

type UnavailabilityReason string

const (
	UnavailabilityReasonPolicy  UnavailabilityReason = "policy"
	UnavailabilityReasonRuntime UnavailabilityReason = "runtime"
)

type Availability struct {
	Available            bool                `json:"available"`
	Version              string              `json:"version"`
	Message              string              `json:"message"`
	UnavailabilityReason UnavailabilityReason `json:"-"`
}

type Server struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Host     string  `json:"host"`
	Sponsor  string  `json:"sponsor"`
	Location string  `json:"location"`
	Country  string  `json:"country"`
	Distance float64 `json:"distanceKilometers,omitempty"`
	URL      string  `json:"url,omitempty"`
}

type TestTarget struct {
	SelectionMode          string         `json:"selectionMode"`
	ServerID               string         `json:"serverId"`
	ServerURL              string         `json:"serverUrl"`
	CustomServerDefinition map[string]any `json:"customServerDefinition"`
}

type ServerListRequest struct {
	InterfaceName string
	SourceIP      string
	IPFamily      string
	Search        string
	Limit         int
}

type RunRequest struct {
	TaskID         string
	InterfaceName  string
	SourceIP       string
	IPFamily       string
	Target         TestTarget
	TimeoutSeconds int
	Options        map[string]any
}

type ProviderResult struct {
	DownloadBitsPerSecond   *int64
	UploadBitsPerSecond     *int64
	LatencyMilliseconds     *float64
	JitterMilliseconds      *float64
	PacketLossPercent       *float64
	DownloadBytes           *int64
	UploadBytes             *int64
	PublicIP                string
	Server                  Server
	ResultURL               string
	CloudflareColo          string
	DurationMilliseconds    int64
	ExitCode                *int
	RawResponse             string
	ProviderVersion         string
	TLSVerificationDisabled bool
}

type Provider interface {
	ID() models.ProviderID
	DisplayName() string
	Capabilities() Capabilities
	Availability(context.Context) Availability
	ListServers(context.Context, ServerListRequest) ([]Server, error)
	Validate(context.Context, TestTarget) error
	Run(context.Context, RunRequest) (ProviderResult, error)
	Version(context.Context) (string, error)
}

type Descriptor struct {
	ID           models.ProviderID `json:"id"`
	DisplayName  string            `json:"displayName"`
	Capabilities Capabilities      `json:"capabilities"`
	Available    bool              `json:"available"`
	Version      string            `json:"version"`
	Message      string            `json:"message"`
}

type Registry struct {
	mu        sync.RWMutex
	providers map[models.ProviderID]Provider
}

func NewRegistry(items ...Provider) *Registry {
	registry := &Registry{providers: make(map[models.ProviderID]Provider, len(items))}
	for _, item := range items {
		registry.providers[item.ID()] = item
	}
	return registry
}

func (r *Registry) Get(id models.ProviderID) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("provider %q: %w", id, ErrUnsupportedProvider)
	}
	return provider, nil
}

func (r *Registry) Descriptors(ctx context.Context) []Descriptor {
	r.mu.RLock()
	items := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		items = append(items, provider)
	}
	r.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].ID() < items[j].ID() })
	descriptors := make([]Descriptor, len(items))
	var waitGroup sync.WaitGroup
	for index, provider := range items {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			providerContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			availability := provider.Availability(providerContext)
			descriptors[index] = Descriptor{ID: provider.ID(), DisplayName: provider.DisplayName(), Capabilities: provider.Capabilities(), Available: availability.Available, Version: availability.Version, Message: availability.Message}
		}()
	}
	waitGroup.Wait()
	return descriptors
}

var (
	ErrUnavailable         = errors.New("provider unavailable")
	ErrUnsupportedProvider = errors.New("unsupported provider")
	ErrInvalidTarget       = errors.New("invalid provider target")
)

// CustomServerURLPolicy is the deployment-owned allowlist for custom provider
// backends. Its zero value authorizes no URLs.
type CustomServerURLPolicy struct {
	allowed map[string]string
}

// NewCustomServerURLPolicy validates and canonicalizes every configured URL.
// Duplicate canonical URLs are collapsed.
func NewCustomServerURLPolicy(entries []string) (CustomServerURLPolicy, error) {
	policy := CustomServerURLPolicy{allowed: make(map[string]string, len(entries))}
	for index, entry := range entries {
		canonical, err := canonicalCustomServerURL(entry)
		if err != nil {
			return CustomServerURLPolicy{}, fmt.Errorf("allowed custom server URL %d: %w", index+1, err)
		}
		policy.allowed[canonical] = canonical
	}
	return policy, nil
}

// Enabled reports whether the deployment authorizes at least one custom URL.
func (policy CustomServerURLPolicy) Enabled() bool {
	return len(policy.allowed) > 0
}

// URLs returns the canonical allowlist in deterministic order.
func (policy CustomServerURLPolicy) URLs() []string {
	items := make([]string, 0, len(policy.allowed))
	for _, item := range policy.allowed {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}

// Authorize returns the deployment-owned canonical URL only when rawURL is an
// exact canonical match. Plain HTTP additionally requires the task's explicit
// allowInsecure opt-in.
func (policy CustomServerURLPolicy) Authorize(rawURL string, allowInsecure bool) (string, error) {
	canonical, err := canonicalCustomServerURL(rawURL)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(canonical, "http://") && !allowInsecure {
		return "", fmt.Errorf("custom server URL must use HTTPS unless an explicitly insecure endpoint definition is used: %w", ErrInvalidTarget)
	}
	authorizedURL, authorized := policy.allowed[canonical]
	if !authorized {
		return "", fmt.Errorf("custom server URL is not authorized by the deployment allowlist: %w", ErrInvalidTarget)
	}
	// Return the deployment-owned value, not the request-derived lookup key.
	return authorizedURL, nil
}

func ValidateHTTPURL(raw string, allowInsecure bool) error {
	canonical, err := canonicalCustomServerURL(raw)
	if err != nil {
		return err
	}
	if strings.HasPrefix(canonical, "http://") && !allowInsecure {
		return fmt.Errorf("custom server URL must use HTTPS unless an explicitly insecure endpoint definition is used: %w", ErrInvalidTarget)
	}
	return nil
}

func canonicalCustomServerURL(raw string) (string, error) {
	if raw == "" || len(raw) > maxCustomServerURLBytes || strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, unsafeURLRune) >= 0 {
		return "", invalidCustomServerURL("custom server URL is invalid")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return "", invalidCustomServerURL("custom server URL must be an absolute HTTP or HTTPS URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalidCustomServerURL("custom server URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return "", invalidCustomServerURL("custom server URL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", invalidCustomServerURL("custom server URL must not contain a query or fragment")
	}

	host, ipv6, err := canonicalCustomServerHost(parsed)
	if err != nil {
		return "", err
	}
	port, err := canonicalCustomServerPort(parsed, scheme)
	if err != nil {
		return "", err
	}
	path, err := canonicalCustomServerPath(parsed)
	if err != nil {
		return "", err
	}

	authority := host
	if ipv6 {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}
	return scheme + "://" + authority + path, nil
}

func canonicalCustomServerHost(parsed *url.URL) (host string, ipv6 bool, err error) {
	rawHost := parsed.Hostname()
	if rawHost == "" || strings.Contains(rawHost, "%") {
		return "", false, invalidCustomServerURL("custom server URL contains an invalid hostname")
	}
	if address, parseErr := netip.ParseAddr(rawHost); parseErr == nil {
		if address.Zone() != "" || address.Is4In6() {
			return "", false, invalidCustomServerURL("custom server URL contains an ambiguous or zoned IP address")
		}
		return address.String(), address.Is6(), nil
	}
	if strings.Contains(rawHost, ":") {
		return "", false, invalidCustomServerURL("custom server URL contains an invalid IP address")
	}
	host = strings.ToLower(rawHost)
	if !validCustomServerHostname(host) || numericHostnameLookalike(host) {
		return "", false, invalidCustomServerURL("custom server URL contains an invalid hostname")
	}
	return host, false, nil
}

func canonicalCustomServerPort(parsed *url.URL, scheme string) (string, error) {
	if strings.HasSuffix(parsed.Host, ":") {
		return "", invalidCustomServerURL("custom server URL contains an invalid port")
	}
	port := parsed.Port()
	if port == "" {
		return "", nil
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", invalidCustomServerURL("custom server URL contains an invalid port")
	}
	if scheme == "http" && value == 80 || scheme == "https" && value == 443 {
		return "", nil
	}
	return strconv.Itoa(value), nil
}

func canonicalCustomServerPath(parsed *url.URL) (string, error) {
	escaped := parsed.EscapedPath()
	if strings.Contains(escaped, "%") || strings.Contains(parsed.Path, "\\") {
		return "", invalidCustomServerURL("custom server URL contains an unsafe path")
	}
	path := parsed.Path
	if path == "" || path == "/" {
		return "", nil
	}
	if !strings.HasPrefix(path, "/") {
		return "", invalidCustomServerURL("custom server URL contains an unsafe path")
	}
	path = strings.TrimSuffix(path, "/")
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." || !validCustomServerPathSegment(segment) {
			return "", invalidCustomServerURL("custom server URL contains an unsafe path")
		}
	}
	return path, nil
}

func validCustomServerHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < '0' || character > '9') && (character < 'a' || character > 'z') {
				return false
			}
		}
	}
	return true
}

func numericHostnameLookalike(host string) bool {
	for _, character := range host {
		if character != '.' && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validCustomServerPathSegment(segment string) bool {
	for _, character := range segment {
		if character > 127 || character != '-' && character != '.' && character != '_' && character != '~' &&
			(character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func unsafeURLRune(character rune) bool {
	return character < 0x20 || character == 0x7f
}

func invalidCustomServerURL(message string) error {
	return fmt.Errorf("%s: %w", message, ErrInvalidTarget)
}

func SanitizeOutput(value string, limit int) string {
	if limit < 1 || limit > MaxStoredOutput {
		limit = MaxStoredOutput
	}
	value = strings.Map(func(r rune) rune {
		if r == '\x00' || (r < 32 && r != '\n' && r != '\r' && r != '\t') {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit] + "…[truncated]"
	}
	return value
}

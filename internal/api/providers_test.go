package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/go-chi/chi/v5"
)

func TestValidateProviderPathInputRequiresConcreteMatchingSource(t *testing.T) {
	validator := sourceValidatorStub{validate: func(interfaceName, sourceIP string) error {
		if interfaceName != "wan0" || sourceIP != "192.0.2.10" {
			t.Fatalf("unexpected path %q %q", interfaceName, sourceIP)
		}
		return nil
	}}
	if err := validateProviderPathInput(validator, "wan0", "192.0.2.10", "ipv4"); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	if err := validateProviderPathInput(validator, "wan0", "192.0.2.10", "ipv6"); err == nil {
		t.Fatal("IPv4 source was accepted for an IPv6-only request")
	}
	if err := validateProviderPathInput(sourceValidatorStub{}, "wan0", "0.0.0.0", "auto"); err == nil {
		t.Fatal("unspecified source was accepted")
	}
	if err := validateProviderPathInput(sourceValidatorStub{}, "wan0", "192.0.2.10", "unexpected"); err == nil {
		t.Fatal("unknown IP family was accepted")
	}
}

func TestConfirmFixedServerUsesBoundDiscoveryAndExactID(t *testing.T) {
	var calls atomic.Int32
	provider := &providerStub{
		capabilities: providers.Capabilities{FixedServerIDs: true, ServerDiscovery: true},
		list: func(_ context.Context, request providers.ServerListRequest) ([]providers.Server, error) {
			calls.Add(1)
			if request.InterfaceName != "wan0" || request.SourceIP != "192.0.2.10" || request.IPFamily != "ipv4" {
				t.Fatalf("discovery was not bound to the requested path: %+v", request)
			}
			if request.Search != "42" || request.Limit != 200 {
				t.Fatalf("fixed server discovery did not use the exact ID: %+v", request)
			}
			return []providers.Server{{ID: "142"}, {ID: "42"}}, nil
		},
	}
	err := confirmFixedServer(context.Background(), provider, "42", providers.ServerListRequest{
		InterfaceName: "wan0",
		SourceIP:      "192.0.2.10",
		IPFamily:      "ipv4",
	})
	if err != nil {
		t.Fatalf("fixed server rejected: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("discovery calls=%d", calls.Load())
	}

	provider.list = func(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
		return []providers.Server{{ID: "142"}}, nil
	}
	if err := confirmFixedServer(context.Background(), provider, "42", providers.ServerListRequest{}); err == nil {
		t.Fatal("a partial server ID match was accepted")
	}
}

func TestConfirmFixedServerSkipsDiscoveryOnlyWhenCapabilityIsAbsent(t *testing.T) {
	var calls atomic.Int32
	provider := &providerStub{
		capabilities: providers.Capabilities{FixedServerIDs: true},
		list: func(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
			calls.Add(1)
			return nil, errors.New("must not be called")
		},
	}
	if err := confirmFixedServer(context.Background(), provider, "42", providers.ServerListRequest{}); err != nil {
		t.Fatalf("provider without discovery should rely on structural validation: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("unexpected discovery calls=%d", calls.Load())
	}
}

func TestCustomTargetHostAcceptsOnlyAbsoluteCredentialFreeHTTPURLs(t *testing.T) {
	for _, test := range []struct {
		url      string
		wantHost string
	}{
		{url: "https://speed.example.test", wantHost: "speed.example.test"},
		{url: "HTTPS://speed.example.test/base", wantHost: "speed.example.test"},
		{url: "http://192.0.2.10:8080/base", wantHost: "192.0.2.10"},
		{url: "https://[2001:db8::10]:8443/base", wantHost: "2001:db8::10"},
	} {
		host, err := customTargetHost(test.url)
		if err != nil || host != test.wantHost {
			t.Fatalf("customTargetHost(%q) = %q, %v; want %q", test.url, host, err, test.wantHost)
		}
	}
	for _, rawURL := range []string{
		"", "speed.example.test", "ftp://speed.example.test", "https://user@speed.example.test",
		"https://speed.example.test\nHost:metadata", "https:///missing-host",
	} {
		if host, err := customTargetHost(rawURL); err == nil {
			t.Fatalf("customTargetHost(%q) unexpectedly returned %q", rawURL, host)
		}
	}
}

func TestListProviderServersFailsClosedBeforeDiscovery(t *testing.T) {
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceService(broker)
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	interfaceName, sourceIP := loopbackPath(t, interfaces)
	var discoveryCalls atomic.Int32
	provider := &providerStub{
		capabilities: providers.Capabilities{ServerDiscovery: true},
		list: func(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
			discoveryCalls.Add(1)
			return nil, nil
		},
	}
	server := &Server{
		interfaces:     interfaces,
		routes:         network.NewRouteValidator(interfaces),
		providers:      providers.NewRegistry(provider),
		discoveryLimit: newRateGate(100, time.Minute),
	}
	request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/providers/test/servers?interface="+
		url.QueryEscape(interfaceName)+"&sourceIp="+url.QueryEscape(sourceIP)+"&ipFamily=ipv4", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("provider", "test")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()

	server.listProviderServers(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if discoveryCalls.Load() != 0 {
		t.Fatalf("provider discovery ran before route validation: calls=%d", discoveryCalls.Load())
	}
}

func TestProviderErrorMessageRemovesControlCharacters(t *testing.T) {
	message := providerErrorMessage(errors.New("provider failed\x00\x01"), "fallback")
	if strings.ContainsAny(message, "\x00\x01") {
		t.Fatalf("unsanitized error message=%q", message)
	}
}

func loopbackPath(t *testing.T, interfaces *network.InterfaceService) (string, string) {
	t.Helper()
	items, _ := interfaces.Snapshot(true, true, true)
	for _, item := range items {
		if !item.Loopback || !item.Operational {
			continue
		}
		for _, address := range item.Addresses {
			if net.ParseIP(address.Address).To4() != nil {
				return item.Name, address.Address
			}
		}
	}
	t.Skip("no operational IPv4 loopback path is available")
	return "", ""
}

type sourceValidatorStub struct {
	validate func(string, string) error
}

func (stub sourceValidatorStub) ValidateSource(interfaceName, sourceIP string) error {
	if stub.validate != nil {
		return stub.validate(interfaceName, sourceIP)
	}
	return nil
}

type providerStub struct {
	id           models.ProviderID
	capabilities providers.Capabilities
	list         func(context.Context, providers.ServerListRequest) ([]providers.Server, error)
	validate     func(context.Context, providers.TestTarget) error
	availability func(context.Context) providers.Availability
}

func (stub *providerStub) ID() models.ProviderID {
	if stub.id != "" {
		return stub.id
	}
	return "test"
}
func (*providerStub) DisplayName() string { return "Test provider" }
func (stub *providerStub) Capabilities() providers.Capabilities {
	return stub.capabilities
}

func (stub *providerStub) Availability(ctx context.Context) providers.Availability {
	if stub.availability != nil {
		return stub.availability(ctx)
	}
	return providers.Availability{Available: true}
}
func (stub *providerStub) ListServers(ctx context.Context, request providers.ServerListRequest) ([]providers.Server, error) {
	if stub.list == nil {
		return nil, nil
	}
	return stub.list(ctx, request)
}
func (stub *providerStub) Validate(ctx context.Context, target providers.TestTarget) error {
	if stub.validate == nil {
		return nil
	}
	return stub.validate(ctx, target)
}
func (*providerStub) Run(context.Context, providers.RunRequest) (providers.ProviderResult, error) {
	return providers.ProviderResult{}, errors.New("speed tests are not used by provider API validation")
}
func (*providerStub) Version(context.Context) (string, error) { return "test", nil }

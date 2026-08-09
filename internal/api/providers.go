package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/go-chi/chi/v5"
)

const (
	providerRequestTimeout   = 45 * time.Second
	providerRouteTimeout     = 15 * time.Second
	providerDiscoveryTimeout = 25 * time.Second
	providerProbeTimeout     = 10 * time.Second
	providerErrorLimit       = 1024
)

type providerServerValidationInput struct {
	SelectionMode          string         `json:"selectionMode"`
	ServerID               string         `json:"serverId"`
	ServerURL              string         `json:"serverUrl"`
	CustomServerDefinition map[string]any `json:"customServerDefinition"`
	ProviderOptions        map[string]any `json:"providerOptions"`
	InterfaceName          string         `json:"interfaceName"`
	SourceIP               string         `json:"sourceIp"`
	IPFamily               string         `json:"ipFamily"`
}

type providerPreflightError struct {
	code                 string
	fallback             string
	err                  error
	unavailabilityReason providers.UnavailabilityReason
}

func (err *providerPreflightError) Error() string { return err.err.Error() }
func (err *providerPreflightError) Unwrap() error { return err.err }

func newProviderPreflightError(code, fallback string, err error) error {
	return &providerPreflightError{code: code, fallback: fallback, err: err}
}

func (input providerServerValidationInput) target() providers.TestTarget {
	return providers.TestTarget{
		SelectionMode:          strings.TrimSpace(input.SelectionMode),
		ServerID:               strings.TrimSpace(input.ServerID),
		ServerURL:              strings.TrimSpace(input.ServerURL),
		CustomServerDefinition: input.CustomServerDefinition,
	}
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.providers.Descriptors(r.Context()))
}

func (s *Server) listProviderServers(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryLimit.Allow(r) {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Provider discovery is rate limited.")
		return
	}
	provider, err := s.providers.Get(models.ProviderID(chi.URLParam(r, "provider")))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", providerErrorMessage(err, "The provider was not found."))
		return
	}

	query := r.URL.Query()
	interfaceName, err := providerInterfaceName(query.Get("interface"), query.Get("interfaceName"))
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_NETWORK_PATH", providerErrorMessage(err, "The selected network path is invalid."))
		return
	}
	sourceIP := canonicalProviderSourceIP(query.Get("sourceIp"))
	ipFamily := normalizedIPFamily(query.Get("ipFamily"))
	if err := validateProviderPathInput(s.interfaces, interfaceName, sourceIP, ipFamily); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_NETWORK_PATH", providerErrorMessage(err, "The selected network path is invalid."))
		return
	}
	limit, err := providerServerLimit(query.Get("limit"))
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_PROVIDER_QUERY", providerErrorMessage(err, "The provider query is invalid."))
		return
	}
	search := strings.TrimSpace(query.Get("search"))
	if len(search) > 200 {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_PROVIDER_QUERY", "The search value must not exceed 200 characters.")
		return
	}

	requestContext, cancelRequest := context.WithTimeout(r.Context(), providerRequestTimeout)
	defer cancelRequest()
	if err := s.validateProviderRoute(requestContext, interfaceName, sourceIP, validationTarget(sourceIP)); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_NETWORK_PATH", providerErrorMessage(err, "The selected network path could not be validated."))
		return
	}

	discoveryContext, cancelDiscovery := context.WithTimeout(requestContext, providerDiscoveryTimeout)
	defer cancelDiscovery()
	items, err := provider.ListServers(discoveryContext, providers.ServerListRequest{
		InterfaceName: interfaceName,
		SourceIP:      sourceIP,
		IPFamily:      ipFamily,
		Search:        search,
		Limit:         limit,
	})
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "PROVIDER_DISCOVERY_FAILED", providerErrorMessage(err, "Provider server discovery failed."))
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) validateProviderServer(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryLimit.Allow(r) {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Provider validation is rate limited.")
		return
	}
	provider, err := s.providers.Get(models.ProviderID(chi.URLParam(r, "provider")))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "PROVIDER_NOT_FOUND", providerErrorMessage(err, "The provider was not found."))
		return
	}
	var input providerServerValidationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	requestContext, cancelRequest := context.WithTimeout(r.Context(), providerRequestTimeout)
	defer cancelRequest()
	if err := s.preflightProviderTarget(requestContext, provider, input); err != nil {
		var failure *providerPreflightError
		if errors.As(err, &failure) {
			writeError(w, r, http.StatusUnprocessableEntity, failure.code, providerErrorMessage(failure.err, failure.fallback))
		} else {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_PROVIDER_TARGET", providerErrorMessage(err, "The provider target is invalid."))
		}
		return
	}
	target := input.target()

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":    true,
		"success":  true,
		"message":  "Provider target configuration and selected network path are valid. Endpoint connectivity is verified when the provider runs.",
		"provider": provider.ID(),
		"target":   target,
	})
}

// preflightProviderTarget validates a provider target using only the selected
// source-bound path. It has no rate-gate or persistence side effects so task
// preflight endpoints can reuse exactly the same checks as provider validation.
func (s *Server) preflightProviderTarget(ctx context.Context, provider providers.Provider, input providerServerValidationInput) error {
	input.InterfaceName = strings.TrimSpace(input.InterfaceName)
	input.SourceIP = canonicalProviderSourceIP(input.SourceIP)
	input.IPFamily = normalizedIPFamily(input.IPFamily)
	if err := validateProviderPathInput(s.interfaces, input.InterfaceName, input.SourceIP, input.IPFamily); err != nil {
		return newProviderPreflightError("INVALID_NETWORK_PATH", "The selected network path is invalid.", err)
	}
	availabilityContext, cancelAvailability := context.WithTimeout(ctx, providerProbeTimeout)
	availability := provider.Availability(availabilityContext)
	cancelAvailability()
	if !availability.Available {
		message := providers.SanitizeOutput(availability.Message, providerErrorLimit)
		if message == "" {
			message = "The selected provider is unavailable."
		}
		return &providerPreflightError{
			code:                 "PROVIDER_UNAVAILABLE",
			fallback:             "The selected provider is unavailable.",
			err:                  errors.New(message),
			unavailabilityReason: availability.UnavailabilityReason,
		}
	}
	target := input.target()
	validationContext, cancelValidation := context.WithTimeout(ctx, providerProbeTimeout)
	err := provider.Validate(validationContext, target)
	cancelValidation()
	if err != nil {
		return newProviderPreflightError("INVALID_PROVIDER_TARGET", "The provider target is invalid.", err)
	}

	// Validate the route to the bound DNS service first. This ensures a
	// hostname lookup cannot occur before the selected path has passed a
	// read-only route and reachability check. Provider target authorization is
	// deliberately evaluated even earlier so an unapproved custom hostname
	// cannot trigger DNS or route inspection.
	if err := s.validateProviderRoute(ctx, input.InterfaceName, input.SourceIP, validationTarget(input.SourceIP)); err != nil {
		return newProviderPreflightError("INVALID_NETWORK_PATH", "The selected network path could not be validated.", err)
	}
	if target.SelectionMode == "custom" {
		host, err := customTargetHost(target.ServerURL)
		if err != nil {
			return newProviderPreflightError("INVALID_PROVIDER_TARGET", "The custom provider target is invalid.", err)
		}
		if err := s.validateProviderRoute(ctx, input.InterfaceName, input.SourceIP, host); err != nil {
			return newProviderPreflightError("INVALID_NETWORK_PATH", "The custom provider route could not be validated.", err)
		}
	}

	switch target.SelectionMode {
	case "fixed":
		discoveryContext, cancelDiscovery := context.WithTimeout(ctx, providerDiscoveryTimeout)
		err = confirmFixedServer(discoveryContext, provider, target.ServerID, providers.ServerListRequest{
			InterfaceName: input.InterfaceName,
			SourceIP:      input.SourceIP,
			IPFamily:      input.IPFamily,
		})
		cancelDiscovery()
		if err != nil {
			return newProviderPreflightError("INVALID_PROVIDER_TARGET", "The fixed provider server could not be validated.", err)
		}
	}
	return nil
}

func (s *Server) validateProviderRoute(ctx context.Context, interfaceName, sourceIP, target string) error {
	routeContext, cancel := context.WithTimeout(ctx, providerRouteTimeout)
	defer cancel()
	validation := s.routes.Validate(routeContext, models.RouteProfile{
		InterfaceName:    interfaceName,
		SourceIP:         sourceIP,
		ValidationTarget: target,
	})
	if validation.Success {
		return nil
	}
	if err := routeContext.Err(); err != nil {
		return fmt.Errorf("network path validation timed out: %w", err)
	}
	message := providers.SanitizeOutput(validation.Message, providerErrorLimit)
	if message == "" {
		message = "route validation did not succeed"
	}
	return fmt.Errorf("network path validation failed: %s", message)
}

func providerInterfaceName(primary, alias string) (string, error) {
	primary = strings.TrimSpace(primary)
	alias = strings.TrimSpace(alias)
	if primary != "" && alias != "" && primary != alias {
		return "", errors.New("interface and interfaceName must identify the same network interface")
	}
	if primary != "" {
		return primary, nil
	}
	return alias, nil
}

func normalizedIPFamily(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "auto"
	}
	return value
}

func canonicalProviderSourceIP(value string) string {
	value = strings.TrimSpace(value)
	if parsed := net.ParseIP(value); parsed != nil {
		return parsed.String()
	}
	return value
}

type sourceValidator interface {
	ValidateSource(interfaceName, sourceIP string) error
}

func validateProviderPathInput(interfaces sourceValidator, interfaceName, sourceIP, ipFamily string) error {
	if err := interfaces.ValidateSource(interfaceName, sourceIP); err != nil {
		return err
	}
	ip := net.ParseIP(sourceIP)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return errors.New("sourceIp must be a concrete unicast IP address")
	}
	switch ipFamily {
	case "auto":
		return nil
	case "ipv4":
		if ip.To4() == nil {
			return errors.New("sourceIp is not an IPv4 address")
		}
		return nil
	case "ipv6":
		if ip.To4() != nil {
			return errors.New("sourceIp is not an IPv6 address")
		}
		return nil
	default:
		return errors.New("ipFamily must be auto, ipv4, or ipv6")
	}
}

func providerServerLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 100, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 200 {
		return 0, errors.New("limit must be an integer between 1 and 200")
	}
	return value, nil
}

func confirmFixedServer(ctx context.Context, provider providers.Provider, serverID string, request providers.ServerListRequest) error {
	capabilities := provider.Capabilities()
	if !capabilities.FixedServerIDs {
		return errors.New("this provider does not support fixed server IDs")
	}
	if !capabilities.ServerDiscovery {
		return nil
	}
	request.Search = serverID
	request.Limit = 200
	items, err := provider.ListServers(ctx, request)
	if err != nil {
		return fmt.Errorf("bound server discovery failed: %w", err)
	}
	for _, item := range items {
		if item.ID == serverID {
			return nil
		}
	}
	return fmt.Errorf("server ID %q was not returned by bound provider discovery", serverID)
}

func customTargetHost(rawURL string) (string, error) {
	if len(rawURL) > 2048 || strings.ContainsAny(rawURL, "\x00\r\n") {
		return "", errors.New("custom server URL is invalid")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return "", errors.New("custom server URL must be an absolute HTTP or HTTPS URL without credentials")
	}
	host := parsed.Hostname()
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " /\\\x00\r\n") {
		return "", errors.New("custom server URL contains an invalid hostname")
	}
	return host, nil
}

func providerErrorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fallback + " The bounded operation timed out or was cancelled."
	}
	message := providers.SanitizeOutput(err.Error(), providerErrorLimit)
	if message == "" {
		return fallback
	}
	return message
}

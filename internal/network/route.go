package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

const maxRouteOutput = 64 << 10

type RouteValidator struct {
	interfaces *InterfaceService
	now        func() time.Time
}

func NewRouteValidator(interfaces *InterfaceService) *RouteValidator {
	return &RouteValidator{interfaces: interfaces, now: time.Now}
}

func (v *RouteValidator) Validate(ctx context.Context, profile models.RouteProfile) (result models.RouteValidation) {
	started := v.currentTime()
	result = models.RouteValidation{InterfaceName: profile.InterfaceName, SourceIP: profile.SourceIP, Destination: profile.ValidationTarget, ValidatedAt: started.UTC()}
	defer func() { result.DurationMS = v.currentTime().Sub(started).Milliseconds() }()
	if err := v.interfaces.ValidateSource(profile.InterfaceName, profile.SourceIP); err != nil {
		result.Message = err.Error()
		return result
	}
	if runtime.GOOS != "linux" {
		result.Message = "route lookup is supported only on Linux"
		return result
	}
	target, err := resolveTarget(ctx, profile.ValidationTarget, profile.SourceIP)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.Destination = target
	route, err := linuxRouteLookup(ctx, target, profile.SourceIP, profile.InterfaceName)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.InterfaceName, result.SourceIP, result.Gateway, result.RoutingTable = route.Device, route.Source, route.Gateway, route.Table
	if route.Device != profile.InterfaceName {
		result.Message = fmt.Sprintf("route selected interface %q instead of %q", route.Device, profile.InterfaceName)
		return result
	}
	if !net.ParseIP(route.Source).Equal(net.ParseIP(profile.SourceIP)) {
		result.Message = fmt.Sprintf("route selected source %q instead of %q", route.Source, profile.SourceIP)
		return result
	}
	if profile.ExpectedGateway != "" && !ipAddressesEqual(route.Gateway, profile.ExpectedGateway) {
		result.Message = fmt.Sprintf("route selected gateway %q instead of %q", route.Gateway, profile.ExpectedGateway)
		return result
	}
	if profile.ExpectedRoutingTable != "" && !routingTableMatches(route.Table, profile.ExpectedRoutingTable) {
		result.Message = fmt.Sprintf("route selected table %q instead of %q", route.Table, profile.ExpectedRoutingTable)
		return result
	}
	publicIP, colo, err := boundTrace(ctx, profile.SourceIP)
	if err != nil {
		result.Message = fmt.Sprintf("route lookup passed but bound reachability failed: %v", err)
		return result
	}
	result.DetectedPublicIP, result.Reachable = publicIP, true
	result.Success = true
	result.Message = "selected interface, source address, route, and public reachability validated"
	if colo != "" {
		result.Message += "; Cloudflare colo " + colo
	}
	return result
}

func (v *RouteValidator) currentTime() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

func ipAddressesEqual(actual, expected string) bool {
	actualIP := net.ParseIP(strings.TrimSpace(actual))
	expectedIP := net.ParseIP(strings.TrimSpace(expected))
	return actualIP != nil && expectedIP != nil && actualIP.Equal(expectedIP)
}

func routingTableMatches(actual, expected string) bool {
	actual = strings.TrimSpace(strings.ToLower(actual))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if actual == expected {
		return true
	}
	return (actual == "main" && expected == "254") || (actual == "254" && expected == "main")
}

type routeLookup struct{ Device, Source, Gateway, Table string }

func linuxRouteLookup(ctx context.Context, target, source, interfaceName string) (routeLookup, error) {
	for _, value := range []struct{ name, value string }{{"target", target}, {"source", source}, {"interface", interfaceName}} {
		if strings.ContainsAny(value.value, "\x00\r\n") || strings.TrimSpace(value.value) != value.value {
			return routeLookup{}, fmt.Errorf("invalid %s", value.name)
		}
	}
	command := exec.CommandContext(ctx, "ip", "-j", "route", "get", target, "from", source)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return routeLookup{}, fmt.Errorf("ip route lookup failed: %s", sanitizeText(string(exitErr.Stderr), 2048))
		}
		return routeLookup{}, fmt.Errorf("ip route lookup failed: %w", err)
	}
	if len(output) > maxRouteOutput {
		return routeLookup{}, errors.New("ip route output exceeded safety limit")
	}
	return parseRouteLookup(output)
}

func parseRouteLookup(output []byte) (routeLookup, error) {
	var rows []struct {
		Device          string `json:"dev"`
		PreferredSource string `json:"prefsrc"`
		Source          string `json:"src"`
		From            string `json:"from"`
		Gateway         string `json:"gateway"`
		Table           any    `json:"table"`
	}
	if err := json.Unmarshal(output, &rows); err != nil || len(rows) == 0 {
		return routeLookup{}, errors.New("ip route returned malformed or empty JSON")
	}
	table := ""
	if rows[0].Table != nil {
		table = fmt.Sprint(rows[0].Table)
	}
	if table == "" {
		table = "main"
	}
	selectedSource := rows[0].Source
	if selectedSource == "" {
		selectedSource = rows[0].From
	}
	if selectedSource == "" {
		selectedSource = rows[0].PreferredSource
	}
	return routeLookup{Device: rows[0].Device, Source: selectedSource, Gateway: rows[0].Gateway, Table: table}, nil
}

func resolveTarget(ctx context.Context, target, source string) (string, error) {
	target = strings.TrimSpace(target)
	if ip := net.ParseIP(target); ip != nil {
		return ip.String(), nil
	}
	if target == "" || len(target) > 253 || strings.ContainsAny(target, " /\\\x00\r\n") {
		return "", errors.New("validation target is not a valid hostname or IP")
	}
	sourceIP := net.ParseIP(source)
	if sourceIP == nil {
		return "", errors.New("source IP is invalid")
	}
	resolver, err := NewSourceBoundResolver(sourceIP)
	if err != nil {
		return "", fmt.Errorf("create source-bound resolver: %w", err)
	}
	addresses, err := resolver.LookupIPAddr(ctx, target)
	if err != nil {
		return "", fmt.Errorf("resolve validation target over selected source: %w", err)
	}
	for _, address := range addresses {
		if (sourceIP.To4() != nil) == (address.IP.To4() != nil) {
			return address.IP.String(), nil
		}
	}
	return "", errors.New("validation target has no address matching the selected IP family")
}

func boundTrace(ctx context.Context, source string) (publicIP, colo string, err error) {
	ip := net.ParseIP(source)
	if ip == nil {
		return "", "", errors.New("invalid source address")
	}
	dialer, err := NewSourceBoundDialer(ip, 5*time.Second, 15*time.Second)
	if err != nil {
		return "", "", fmt.Errorf("create source-bound dialer: %w", err)
	}
	networkName := "tcp4"
	if ip.To4() == nil {
		networkName = "tcp6"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, networkName, address)
	},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://speed.cloudflare.com/cdn-cgi/trace", nil)
	if err != nil {
		return "", "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("trace endpoint returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			publicIP = value
		case "colo":
			colo = value
		}
	}
	if net.ParseIP(publicIP) == nil {
		return "", colo, errors.New("trace endpoint did not return a valid public IP")
	}
	return publicIP, colo, nil
}

func sanitizeText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\x00' || (r < 32 && r != '\n' && r != '\t') {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit] + "…"
	}
	return value
}

package api

import (
	"net"
	"strconv"
	"strings"

	"github.com/dude2k/MultiSpeed/internal/network"
)

// HTTPPolicy defines the authorities through which the unauthenticated UI may
// be reached. TrustedHosts contains hostnames or IP literals only; ports are
// governed by ListenAddress.
type HTTPPolicy struct {
	ListenAddress                string
	TrustedHosts                 []string
	OoklaEULAEnvironmentAccepted bool
	DataDirectory                string
	OoklaBinaryPath              string
	AllowOoklaBinaryUpload       bool
}

type hostPolicy struct {
	listenHost string
	listenIP   net.IP
	listenPort int
	wildcard   bool
	trusted    map[string]struct{}
	interfaces *network.InterfaceService
}

func newHostPolicy(policy HTTPPolicy, interfaces *network.InterfaceService) hostPolicy {
	listenAddress := policy.ListenAddress
	if listenAddress == "" {
		listenAddress = "127.0.0.1:8787"
	}
	listenHost, portText, err := net.SplitHostPort(listenAddress)
	if err != nil {
		panic("api: invalid HTTP listen address")
	}
	listenPort, err := strconv.Atoi(portText)
	if err != nil || listenPort < 1 || listenPort > 65535 {
		panic("api: invalid HTTP listen port")
	}
	listenHost = normalizeHost(listenHost)
	listenIP := net.ParseIP(listenHost)
	wildcard := listenHost == "" || (listenIP != nil && listenIP.IsUnspecified())
	trusted := make(map[string]struct{}, len(policy.TrustedHosts))
	for _, host := range policy.TrustedHosts {
		normalized := normalizeHost(host)
		if normalized == "" || !validHostNameOrIP(normalized) {
			panic("api: invalid trusted host")
		}
		trusted[normalized] = struct{}{}
	}
	return hostPolicy{listenHost: listenHost, listenIP: listenIP, listenPort: listenPort, wildcard: wildcard, trusted: trusted, interfaces: interfaces}
}

func (policy hostPolicy) allows(authority string) bool {
	host, port, hasPort, ok := parseAuthority(authority)
	if !ok || (hasPort && port != policy.listenPort) {
		return false
	}
	if _, explicitlyTrusted := policy.trusted[host]; explicitlyTrusted {
		return true
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return !policy.wildcard && policy.listenIP == nil && host == policy.listenHost
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if policy.wildcard {
		return true
	}
	if !policy.wildcard && policy.listenIP != nil && ip.Equal(policy.listenIP) {
		return true
	}
	if policy.interfaces == nil {
		return false
	}
	interfaces, _ := policy.interfaces.Snapshot(true, true, true)
	for _, item := range interfaces {
		for _, address := range item.Addresses {
			if assigned := net.ParseIP(address.Address); assigned != nil && ip.Equal(assigned) {
				return true
			}
		}
	}
	return false
}

func parseAuthority(authority string) (host string, port int, hasPort, ok bool) {
	if authority == "" || strings.TrimSpace(authority) != authority || strings.ContainsAny(authority, "/\\@\r\n\t") {
		return "", 0, false, false
	}
	hostText := authority
	portText := ""
	if strings.HasPrefix(authority, "[") {
		closing := strings.IndexByte(authority, ']')
		if closing < 0 {
			return "", 0, false, false
		}
		hostText = authority[1:closing]
		remainder := authority[closing+1:]
		if remainder != "" {
			if !strings.HasPrefix(remainder, ":") || len(remainder) == 1 {
				return "", 0, false, false
			}
			portText = remainder[1:]
			hasPort = true
		}
	} else if strings.Count(authority, ":") == 1 {
		var err error
		hostText, portText, err = net.SplitHostPort(authority)
		if err != nil {
			return "", 0, false, false
		}
		hasPort = true
	} else if strings.Count(authority, ":") > 1 && net.ParseIP(authority) == nil {
		return "", 0, false, false
	}
	if hasPort {
		parsedPort, err := strconv.Atoi(portText)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", 0, false, false
		}
		port = parsedPort
	}
	host = normalizeHost(hostText)
	if host == "" || !validHostNameOrIP(host) {
		return "", 0, false, false
	}
	return host, port, hasPort, true
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func validHostNameOrIP(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
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

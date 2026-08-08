// SPDX-License-Identifier: LGPL-3.0-or-later
// MultiSpeed modification: make LibreSpeed hostname resolution follow the
// exact source address selected for the measurement sockets.
package speedtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"time"
)

const allowedServerEndpointsEnvironment = "MULTISPEED_PROVIDER_ALLOWED_SERVER_ENDPOINTS"

func newSourceBoundResolver(sourceIP net.IP, resolverOverride ...string) *net.Resolver {
	ipv4 := sourceIP.To4() != nil
	fallbackAddress := "1.1.1.1:53"
	if !ipv4 {
		fallbackAddress = "[2606:4700:4700::1111]:53"
	}
	forcedAddress := ""
	if len(resolverOverride) == 1 {
		// Used only by the build-time integration test. Production passes no
		// override and selects a configured, family-matched resolver below.
		forcedAddress = resolverOverride[0]
	}
	return &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			target := forcedAddress
			if target == "" {
				target = address
				if !resolverMatchesFamily(address, ipv4) {
					target = fallbackAddress
				}
			}

			transport := "udp4"
			var localAddress net.Addr = &net.UDPAddr{IP: sourceIP}
			if !ipv4 {
				transport = "udp6"
			}
			if strings.HasPrefix(network, "tcp") {
				transport = "tcp4"
				if !ipv4 {
					transport = "tcp6"
				}
				localAddress = &net.TCPAddr{IP: sourceIP}
			}
			dialer := net.Dialer{Timeout: 10 * time.Second, LocalAddr: localAddress}
			return dialer.DialContext(ctx, transport, target)
		},
	}
}

func resolverMatchesFamily(address string, ipv4 bool) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
		return false
	}
	return (ip.To4() != nil) == ipv4
}

// restrictDialerToAllowedServerEndpoints pins a custom-server run to the exact
// source-bound IP:port set authorized by MultiSpeed immediately before the CLI
// starts and rejects every redirect before a follow-up request can be sent. The
// environment variable is stripped from inherited process state and can only
// be supplied through the provider runner's guarded environment.
func restrictDialerToAllowedServerEndpoints(dialer *net.Dialer) error {
	raw, restricted := os.LookupEnv(allowedServerEndpointsEnvironment)
	if !restricted {
		return nil
	}
	entries := strings.Split(raw, ",")
	if strings.TrimSpace(raw) == "" || len(entries) > 64 {
		return errors.New("custom server destination guard is empty or too large")
	}
	allowed := make(map[netip.AddrPort]struct{}, len(entries))
	for _, entry := range entries {
		endpoint, err := netip.ParseAddrPort(strings.TrimSpace(entry))
		if err != nil || endpoint.Addr().Zone() != "" || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() || endpoint.Port() == 0 {
			return errors.New("custom server destination guard contains an invalid IP:port endpoint")
		}
		allowed[netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())] = struct{}{}
	}
	if len(allowed) == 0 {
		return errors.New("custom server destination guard has no endpoints")
	}
	http.DefaultClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("custom server redirects are not allowed")
	}
	previous := dialer.ControlContext
	dialer.ControlContext = func(ctx context.Context, network, address string, rawConnection syscall.RawConn) error {
		destination, err := netip.ParseAddrPort(address)
		if err != nil || destination.Addr().Zone() != "" || destination.Port() == 0 {
			return errors.New("custom server dial target was not resolved to a valid IP:port endpoint")
		}
		destination = netip.AddrPortFrom(destination.Addr().Unmap(), destination.Port())
		if _, authorized := allowed[destination]; !authorized {
			return fmt.Errorf("custom server dial target %s changed after authorization", destination)
		}
		if previous != nil {
			return previous(ctx, network, address, rawConnection)
		}
		return nil
	}
	return nil
}

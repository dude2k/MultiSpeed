package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// resolverDialContext is injectable so the binding policy can be tested without
// contacting a DNS server. Production always uses net.Dialer.DialContext.
type resolverDialContext func(context.Context, *net.Dialer, string, string) (net.Conn, error)

const (
	boundDNSFallbackIPv4 = "1.1.1.1:53"
	boundDNSFallbackIPv6 = "[2606:4700:4700::1111]:53"
)

// NewSourceBoundResolver returns a pure-Go resolver whose UDP queries and TCP
// fallback connections both originate from sourceIP. Local stub resolvers and
// system resolvers from the opposite address family cannot safely be reached
// from an arbitrary WAN source, so those cases use a family-matched public DNS
// endpoint while retaining the same mandatory source binding.
func NewSourceBoundResolver(sourceIP net.IP) (*net.Resolver, error) {
	return newSourceBoundResolver(sourceIP, func(ctx context.Context, dialer *net.Dialer, networkName, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, networkName, address)
	})
}

// NewSourceBoundDialer returns a TCP dialer that binds application connections
// and all DNS traffic to the same concrete source address.
func NewSourceBoundDialer(sourceIP net.IP, timeout, keepAlive time.Duration) (*net.Dialer, error) {
	normalized, _, err := normalizeConcreteSourceIP(sourceIP)
	if err != nil {
		return nil, err
	}
	resolver, err := NewSourceBoundResolver(normalized)
	if err != nil {
		return nil, err
	}
	return &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: normalized},
		Timeout:   timeout,
		KeepAlive: keepAlive,
		Resolver:  resolver,
	}, nil
}

func newSourceBoundResolver(sourceIP net.IP, dial resolverDialContext) (*net.Resolver, error) {
	if dial == nil {
		return nil, errors.New("resolver dial function is required")
	}
	normalized, family, err := normalizeConcreteSourceIP(sourceIP)
	if err != nil {
		return nil, err
	}

	return &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, requestedNetwork, address string) (net.Conn, error) {
			transport, _, err := parseResolverNetwork(requestedNetwork)
			if err != nil {
				return nil, err
			}
			canonicalAddress, err := resolverEndpoint(address, family)
			if err != nil {
				return nil, err
			}

			resolverDialer := &net.Dialer{}
			if transport == "udp" {
				resolverDialer.LocalAddr = &net.UDPAddr{IP: cloneIP(normalized)}
			} else {
				resolverDialer.LocalAddr = &net.TCPAddr{IP: cloneIP(normalized)}
			}
			return dial(ctx, resolverDialer, transport+strconv.Itoa(family), canonicalAddress)
		},
	}, nil
}

func resolverEndpoint(address string, family int) (string, error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return "", fmt.Errorf("dns resolver endpoint must be a numeric IP and port: %w", err)
	}
	serverIP := endpoint.Addr().Unmap()
	serverFamily := 6
	if serverIP.Is4() {
		serverFamily = 4
	}
	if serverIP.IsUnspecified() || serverIP.IsMulticast() {
		return "", errors.New("dns resolver endpoint must be a concrete unicast address")
	}
	if serverFamily != family || serverIP.IsLoopback() {
		if family == 4 {
			return boundDNSFallbackIPv4, nil
		}
		return boundDNSFallbackIPv6, nil
	}
	return net.JoinHostPort(serverIP.String(), strconv.Itoa(int(endpoint.Port()))), nil
}

func normalizeConcreteSourceIP(sourceIP net.IP) (net.IP, int, error) {
	if sourceIP == nil {
		return nil, 0, errors.New("source IP is required")
	}
	var normalized net.IP
	family := 6
	if ipv4 := sourceIP.To4(); ipv4 != nil {
		normalized = cloneIP(ipv4)
		family = 4
	} else if ipv6 := sourceIP.To16(); ipv6 != nil {
		normalized = cloneIP(ipv6)
	} else {
		return nil, 0, errors.New("source IP is invalid")
	}
	if normalized.IsUnspecified() || normalized.IsMulticast() || normalized.Equal(net.IPv4bcast) {
		return nil, 0, errors.New("source IP must be a concrete unicast address")
	}
	return normalized, family, nil
}

func parseResolverNetwork(networkName string) (transport string, family int, err error) {
	switch networkName {
	case "udp":
		return "udp", 0, nil
	case "udp4":
		return "udp", 4, nil
	case "udp6":
		return "udp", 6, nil
	case "tcp":
		return "tcp", 0, nil
	case "tcp4":
		return "tcp", 4, nil
	case "tcp6":
		return "tcp", 6, nil
	default:
		return "", 0, fmt.Errorf("unsupported DNS resolver network %q", networkName)
	}
}

func cloneIP(ip net.IP) net.IP {
	return append(net.IP(nil), ip...)
}

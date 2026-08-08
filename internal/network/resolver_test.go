package network

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestSourceBoundResolverBindsUDPAndTCPByAddressFamily(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		requested       string
		resolverAddress string
		wantNetwork     string
		wantAddress     string
		wantLocalType   string
	}{
		{name: "IPv4 UDP", source: "192.0.2.10", requested: "udp", resolverAddress: "192.0.2.53:53", wantNetwork: "udp4", wantAddress: "192.0.2.53:53", wantLocalType: "udp"},
		{name: "IPv4 TCP fallback", source: "192.0.2.10", requested: "tcp4", resolverAddress: "192.0.2.53:53", wantNetwork: "tcp4", wantAddress: "192.0.2.53:53", wantLocalType: "tcp"},
		{name: "IPv6 UDP", source: "2001:db8::10", requested: "udp6", resolverAddress: "[2001:0db8::53]:53", wantNetwork: "udp6", wantAddress: "[2001:db8::53]:53", wantLocalType: "udp"},
		{name: "IPv6 TCP fallback", source: "2001:db8::10", requested: "tcp", resolverAddress: "[2001:db8::53]:53", wantNetwork: "tcp6", wantAddress: "[2001:db8::53]:53", wantLocalType: "tcp"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var observedDialer *net.Dialer
			var observedNetwork, observedAddress string
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "forwarded")
			sentinel := errors.New("dial intercepted")
			resolver, err := newSourceBoundResolver(net.ParseIP(test.source), func(gotContext context.Context, dialer *net.Dialer, networkName, address string) (net.Conn, error) {
				if gotContext.Value(contextKey{}) != "forwarded" {
					t.Fatal("resolver dial did not receive the caller context")
				}
				observedDialer, observedNetwork, observedAddress = dialer, networkName, address
				return nil, sentinel
			})
			if err != nil {
				t.Fatal(err)
			}
			if !resolver.PreferGo || !resolver.StrictErrors {
				t.Fatalf("resolver safety flags: PreferGo=%t StrictErrors=%t", resolver.PreferGo, resolver.StrictErrors)
			}

			_, err = resolver.Dial(ctx, test.requested, test.resolverAddress)
			if !errors.Is(err, sentinel) {
				t.Fatalf("Dial error=%v, want intercepted sentinel", err)
			}
			if observedNetwork != test.wantNetwork || observedAddress != test.wantAddress {
				t.Fatalf("dial target=(%q, %q), want=(%q, %q)", observedNetwork, observedAddress, test.wantNetwork, test.wantAddress)
			}

			var localIP net.IP
			switch local := observedDialer.LocalAddr.(type) {
			case *net.UDPAddr:
				if test.wantLocalType != "udp" {
					t.Fatalf("local address type=%T, want TCP", local)
				}
				localIP = local.IP
			case *net.TCPAddr:
				if test.wantLocalType != "tcp" {
					t.Fatalf("local address type=%T, want UDP", local)
				}
				localIP = local.IP
			default:
				t.Fatalf("local address type=%T", observedDialer.LocalAddr)
			}
			if !localIP.Equal(net.ParseIP(test.source)) {
				t.Fatalf("local source=%s, want %s", localIP, test.source)
			}
		})
	}
}

func TestSourceBoundResolverRejectsUnboundFallbacks(t *testing.T) {
	resolver, err := newSourceBoundResolver(net.ParseIP("192.0.2.10"), func(context.Context, *net.Dialer, string, string) (net.Conn, error) {
		t.Fatal("unsafe resolver request reached the network dial function")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name, networkName, address string
	}{
		{name: "hostname resolver endpoint", networkName: "udp", address: "resolver.example:53"},
		{name: "unsupported transport", networkName: "unix", address: "192.0.2.53:53"},
		{name: "multicast resolver endpoint", networkName: "udp", address: "224.0.0.53:53"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := resolver.Dial(context.Background(), test.networkName, test.address); err == nil {
				t.Fatal("unsafe resolver request unexpectedly succeeded")
			}
		})
	}
}

func TestSourceBoundResolverUsesFamilyMatchedBoundFallback(t *testing.T) {
	tests := []struct {
		name, source, networkName, resolverAddress, wantNetwork, wantAddress string
	}{
		{name: "IPv4 source with local stub", source: "192.0.2.10", networkName: "udp", resolverAddress: "127.0.0.53:53", wantNetwork: "udp4", wantAddress: boundDNSFallbackIPv4},
		{name: "IPv4 source with IPv6 resolver", source: "192.0.2.10", networkName: "tcp6", resolverAddress: "[2001:db8::53]:53", wantNetwork: "tcp4", wantAddress: boundDNSFallbackIPv4},
		{name: "IPv6 source with local stub", source: "2001:db8::10", networkName: "udp4", resolverAddress: "127.0.0.53:53", wantNetwork: "udp6", wantAddress: boundDNSFallbackIPv6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var observedNetwork, observedAddress string
			sentinel := errors.New("dial intercepted")
			resolver, err := newSourceBoundResolver(net.ParseIP(test.source), func(_ context.Context, dialer *net.Dialer, networkName, address string) (net.Conn, error) {
				observedNetwork, observedAddress = networkName, address
				if dialer.LocalAddr == nil {
					t.Fatal("fallback resolver dial was not source-bound")
				}
				return nil, sentinel
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = resolver.Dial(context.Background(), test.networkName, test.resolverAddress)
			if !errors.Is(err, sentinel) {
				t.Fatalf("Dial error=%v, want intercepted sentinel", err)
			}
			if observedNetwork != test.wantNetwork || observedAddress != test.wantAddress {
				t.Fatalf("dial target=(%q, %q), want=(%q, %q)", observedNetwork, observedAddress, test.wantNetwork, test.wantAddress)
			}
		})
	}
}

func TestSourceBoundDialerConstruction(t *testing.T) {
	source := net.ParseIP("192.0.2.25")
	dialer, err := NewSourceBoundDialer(source, 7*time.Second, 19*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for index := range source {
		source[index] = 0
	}

	local, ok := dialer.LocalAddr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("LocalAddr type=%T, want *net.TCPAddr", dialer.LocalAddr)
	}
	if got := local.IP.String(); got != "192.0.2.25" {
		t.Fatalf("bound source=%s, want 192.0.2.25", got)
	}
	if dialer.Timeout != 7*time.Second || dialer.KeepAlive != 19*time.Second {
		t.Fatalf("timeouts=(%v, %v)", dialer.Timeout, dialer.KeepAlive)
	}
	if dialer.Resolver == nil || !dialer.Resolver.PreferGo || !dialer.Resolver.StrictErrors || dialer.Resolver.Dial == nil {
		t.Fatalf("source-bound resolver was not installed: %#v", dialer.Resolver)
	}
}

func TestSourceBoundFactoriesRejectNonConcreteSources(t *testing.T) {
	for _, source := range []net.IP{
		nil,
		net.ParseIP("0.0.0.0"),
		net.ParseIP("::"),
		net.ParseIP("224.0.0.1"),
		net.ParseIP("ff02::1"),
		net.ParseIP("255.255.255.255"),
	} {
		if _, err := NewSourceBoundResolver(source); err == nil {
			t.Fatalf("resolver accepted non-concrete source %v", source)
		}
		if _, err := NewSourceBoundDialer(source, time.Second, time.Second); err == nil {
			t.Fatalf("dialer accepted non-concrete source %v", source)
		}
	}
}

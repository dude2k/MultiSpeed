package api

import (
	"context"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
)

func TestWildcardHostPolicyAllowsUnicastIPsButRequiresExplicitDNSNames(t *testing.T) {
	interfaces := network.NewInterfaceServiceWithDiscoverer(nil, func(context.Context) ([]models.NetworkInterface, error) {
		return []models.NetworkInterface{{
			Name: "wan-test", Operational: true,
			Addresses: []models.InterfaceAddress{{Address: "192.0.2.25", Family: "ipv4"}, {Address: "2001:db8::25", Family: "ipv6"}},
		}}, nil
	})
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh interfaces: %v", err)
	}
	policy := newHostPolicy(HTTPPolicy{
		ListenAddress: "0.0.0.0:8787",
		TrustedHosts:  []string{"speed.example.test"},
	}, interfaces)

	for _, authority := range []string{"192.0.2.25:8787", "198.51.100.30:8787", "[2001:db8::25]:8787", "127.0.0.1:8787", "speed.example.test:8787"} {
		if !policy.allows(authority) {
			t.Errorf("expected authority %q to be allowed", authority)
		}
	}
	for _, authority := range []string{"0.0.0.0:8787", "[::]:8787", "speed.example.test:9999", "lan-router:8787", "attacker.example:8787", "bad host:8787", "[2001:db8::25"} {
		if policy.allows(authority) {
			t.Errorf("expected authority %q to be rejected", authority)
		}
	}
}

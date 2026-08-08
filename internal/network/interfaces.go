// Package network provides read-only interface discovery and route validation.
package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/models"
)

type InterfaceService struct {
	mu          sync.RWMutex
	items       []models.NetworkInterface
	refreshedAt time.Time
	broker      *events.Broker
	discover    func(context.Context) ([]models.NetworkInterface, error)
}

func NewInterfaceService(broker *events.Broker) *InterfaceService {
	return &InterfaceService{broker: broker, items: make([]models.NetworkInterface, 0)}
}

// NewInterfaceServiceWithDiscoverer supplies an alternate namespace snapshot
// source for deterministic integration environments. Production always uses
// NewInterfaceService and the active Linux namespace.
func NewInterfaceServiceWithDiscoverer(broker *events.Broker, discover func(context.Context) ([]models.NetworkInterface, error)) *InterfaceService {
	return &InterfaceService{broker: broker, items: make([]models.NetworkInterface, 0), discover: discover}
}

func (s *InterfaceService) Refresh(ctx context.Context) ([]models.NetworkInterface, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if s.discover != nil {
		items, err := s.discover(ctx)
		if err != nil {
			return nil, err
		}
		return s.commitSnapshot(items), nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate network interfaces: %w", err)
	}
	items := make([]models.NetworkInterface, 0, len(interfaces))
	for _, iface := range interfaces {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		operational, operationalState := interfaceOperationalState(iface)
		item := models.NetworkInterface{
			Name: iface.Name, Index: iface.Index, MTU: iface.MTU, MACAddress: iface.HardwareAddr.String(),
			Operational: operational, OperationalState: operationalState, Loopback: iface.Flags&net.FlagLoopback != 0,
			Virtual: looksVirtual(iface.Name), Addresses: make([]models.InterfaceAddress, 0, len(addresses)),
		}
		for _, address := range addresses {
			raw := address.String()
			ipText := raw
			if host, _, err := net.ParseCIDR(raw); err == nil {
				ipText = host.String()
			}
			ip := net.ParseIP(ipText)
			if ip == nil {
				continue
			}
			family := "ipv6"
			if ip.To4() != nil {
				family = "ipv4"
			}
			item.Addresses = append(item.Addresses, models.InterfaceAddress{Address: ip.String(), Family: family, LinkLocal: ip.IsLinkLocalUnicast()})
		}
		sort.Slice(item.Addresses, func(i, j int) bool { return item.Addresses[i].Address < item.Addresses[j].Address })
		items = append(items, item)
	}
	return s.commitSnapshot(items), nil
}

func (s *InterfaceService) commitSnapshot(items []models.NetworkInterface) []models.NetworkInterface {
	items = cloneInterfaces(items)
	for index := range items {
		sort.Slice(items[index].Addresses, func(first, second int) bool {
			return items[index].Addresses[first].Address < items[index].Addresses[second].Address
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	s.mu.Lock()
	changed := !interfaceSnapshotsEqual(s.items, items)
	s.items = items
	s.refreshedAt = time.Now().UTC()
	s.mu.Unlock()
	if changed && s.broker != nil {
		s.broker.Publish("interface.state.changed", map[string]any{"interfaces": items})
	}
	return cloneInterfaces(items)
}

func interfaceOperationalState(iface net.Interface) (bool, string) {
	administrativelyUp := iface.Flags&net.FlagUp != 0
	fallback := "down"
	if administrativelyUp {
		fallback = "unknown"
	}
	if runtime.GOOS != "linux" || strings.ContainsAny(iface.Name, `/\\\x00`) {
		return administrativelyUp, fallback
	}
	raw, err := os.ReadFile(filepath.Join("/sys/class/net", iface.Name, "operstate"))
	if err != nil {
		return administrativelyUp, fallback
	}
	state := strings.TrimSpace(string(raw))
	operational := administrativelyUp && (state == "up" || state == "unknown")
	return operational, state
}

func (s *InterfaceService) Snapshot(includeLoopback, includeDown, includeVirtual bool) ([]models.NetworkInterface, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]models.NetworkInterface, 0, len(s.items))
	for _, item := range s.items {
		if item.Loopback && !includeLoopback {
			continue
		}
		if !item.Operational && !includeDown {
			continue
		}
		if item.Virtual && !includeVirtual {
			continue
		}
		filtered = append(filtered, item)
	}
	return cloneInterfaces(filtered), s.refreshedAt
}

func (s *InterfaceService) ValidateSource(interfaceName, sourceIP string) error {
	if strings.TrimSpace(interfaceName) == "" {
		return fmt.Errorf("network interface is required")
	}
	ip := net.ParseIP(sourceIP)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("source IP %q is not a usable concrete address", sourceIP)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.Name != interfaceName {
			continue
		}
		if !item.Operational {
			return fmt.Errorf("network interface %q is down", interfaceName)
		}
		for _, address := range item.Addresses {
			if address.Address == ip.String() {
				return nil
			}
		}
		return fmt.Errorf("source IP %q is not assigned to interface %q", sourceIP, interfaceName)
	}
	return fmt.Errorf("network interface %q does not exist", interfaceName)
}

func looksVirtual(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"br-", "docker", "veth", "virbr", "vmnet", "wg", "tun", "tap", "tailscale", "zt"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func interfaceSnapshotsEqual(a, b []models.NetworkInterface) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Operational != b[i].Operational || a[i].OperationalState != b[i].OperationalState || a[i].MACAddress != b[i].MACAddress || len(a[i].Addresses) != len(b[i].Addresses) {
			return false
		}
		for j := range a[i].Addresses {
			if a[i].Addresses[j] != b[i].Addresses[j] {
				return false
			}
		}
	}
	return true
}

func cloneInterfaces(items []models.NetworkInterface) []models.NetworkInterface {
	result := make([]models.NetworkInterface, len(items))
	for i, item := range items {
		result[i] = item
		result[i].Addresses = append([]models.InterfaceAddress(nil), item.Addresses...)
	}
	return result
}

package network

import (
	"context"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestRouteValidatorRecordsDurationOnEarlyFailure(t *testing.T) {
	started := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	times := []time.Time{started, started.Add(42 * time.Millisecond)}
	clockCall := 0
	validator := &RouteValidator{
		interfaces: NewInterfaceService(nil),
		now: func() time.Time {
			if clockCall >= len(times) {
				t.Fatalf("clock called more than %d times", len(times))
			}
			value := times[clockCall]
			clockCall++
			return value
		},
	}

	result := validator.Validate(context.Background(), models.RouteProfile{SourceIP: "192.0.2.1"})
	if result.Success {
		t.Fatal("validation unexpectedly succeeded")
	}
	if result.DurationMS != 42 {
		t.Fatalf("duration=%dms, want 42ms", result.DurationMS)
	}
}

func TestIPAddressesEqualUsesCanonicalAddressValue(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{name: "expanded and compressed IPv6", actual: "2001:0DB8:0000:0000:0000:0000:0000:0001", expected: "2001:db8::1", want: true},
		{name: "same IPv4", actual: "192.0.2.1", expected: "192.0.2.1", want: true},
		{name: "surrounding whitespace", actual: " 2001:db8::1 ", expected: "2001:db8::1", want: true},
		{name: "different addresses", actual: "2001:db8::1", expected: "2001:db8::2", want: false},
		{name: "missing actual gateway", actual: "", expected: "2001:db8::1", want: false},
		{name: "malformed gateway", actual: "not-an-ip", expected: "2001:db8::1", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ipAddressesEqual(test.actual, test.expected); got != test.want {
				t.Fatalf("ipAddressesEqual(%q, %q)=%t, want %t", test.actual, test.expected, got, test.want)
			}
		})
	}
}

func TestParseRouteLookupAcceptsExplicitFromSource(t *testing.T) {
	lookup, err := parseRouteLookup([]byte(`[{"dst":"1.1.1.1","from":"192.0.2.10","gateway":"192.0.2.1","dev":"wan0","table":100}]`))
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Device != "wan0" || lookup.Source != "192.0.2.10" || lookup.Gateway != "192.0.2.1" || lookup.Table != "100" {
		t.Fatalf("lookup=%+v", lookup)
	}
}

func TestParseRouteLookupSourcePrecedenceAndMainDefault(t *testing.T) {
	lookup, err := parseRouteLookup([]byte(`[{"dev":"wan0","src":"192.0.2.11","from":"192.0.2.10","prefsrc":"192.0.2.12"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Source != "192.0.2.11" || lookup.Table != "main" {
		t.Fatalf("lookup=%+v", lookup)
	}
}

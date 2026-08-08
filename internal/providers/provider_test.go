package providers

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCustomServerURLPolicyCanonicalizesDeduplicatesAndAuthorizes(t *testing.T) {
	policy, err := NewCustomServerURLPolicy([]string{
		"HTTPS://Speed.Example.test:443/librespeed/",
		"https://speed.example.test/librespeed",
		"http://192.0.2.20:8080/speed",
		"https://[2001:db8:0:0::20]:443/",
	})
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if !policy.Enabled() {
		t.Fatal("non-empty policy is disabled")
	}
	wantURLs := []string{
		"http://192.0.2.20:8080/speed",
		"https://[2001:db8::20]",
		"https://speed.example.test/librespeed",
	}
	if got := policy.URLs(); !reflect.DeepEqual(got, wantURLs) {
		t.Fatalf("policy URLs = %#v, want %#v", got, wantURLs)
	}

	canonical, err := policy.Authorize("https://SPEED.example.test:443/librespeed/", false)
	if err != nil || canonical != "https://speed.example.test/librespeed" {
		t.Fatalf("authorize HTTPS = %q, %v", canonical, err)
	}
	canonical, err = policy.Authorize("http://192.0.2.20:08080/speed/", true)
	if err != nil || canonical != "http://192.0.2.20:8080/speed" {
		t.Fatalf("authorize opted-in HTTP = %q, %v", canonical, err)
	}
	if _, err := policy.Authorize("http://192.0.2.20:8080/speed", false); err == nil || !errors.Is(err, ErrInvalidTarget) || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("HTTP without opt-in error = %v", err)
	}
	if _, err := policy.Authorize("https://other.example.test/librespeed", false); err == nil || !errors.Is(err, ErrInvalidTarget) || !strings.Contains(err.Error(), "deployment allowlist") {
		t.Fatalf("unlisted URL error = %v", err)
	}
}

func TestCustomServerURLPolicyZeroValueIsFailClosed(t *testing.T) {
	var policy CustomServerURLPolicy
	if policy.Enabled() || policy.URLs() == nil {
		t.Fatalf("zero policy state: enabled=%v URLs=%#v", policy.Enabled(), policy.URLs())
	}
	if _, err := policy.Authorize("https://speed.example.test", false); err == nil || !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("zero policy authorization error = %v", err)
	}
}

func TestCustomServerURLPolicyRejectsUnsafeEntries(t *testing.T) {
	tests := map[string]string{
		"empty":                  "",
		"surrounding whitespace": " https://speed.example.test",
		"relative":               "/speed",
		"unsupported scheme":     "ftp://speed.example.test",
		"credentials":            "https://user:secret@speed.example.test",
		"query":                  "https://speed.example.test/path?target=other",
		"empty query":            "https://speed.example.test/path?",
		"fragment":               "https://speed.example.test/path#fragment",
		"IPv6 zone":              "https://[fe80::1%25eth0]/",
		"mapped IPv4":            "https://[::ffff:127.0.0.1]/",
		"trailing host dot":      "https://speed.example.test./",
		"underscore host":        "https://speed_test.example.test/",
		"unicode host":           "https://sp\u00e9ed.example.test/",
		"numeric lookalike":      "https://127.0.0.01/",
		"empty port":             "https://speed.example.test:/",
		"invalid port":           "https://speed.example.test:65536/",
		"double slash path":      "https://speed.example.test/a//b",
		"dot segment":            "https://speed.example.test/a/../b",
		"encoded dot segment":    "https://speed.example.test/%2e%2e/admin",
		"encoded slash":          "https://speed.example.test/a%2fb",
		"space in path":          "https://speed.example.test/a%20b",
		"path parameter":         "https://speed.example.test/a;admin",
		"backslash path":         "https://speed.example.test/a\\b",
		"control character":      "https://speed.example.test/a\nb",
		"oversized":              "https://speed.example.test/" + strings.Repeat("a", maxCustomServerURLBytes),
	}
	for name, rawURL := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCustomServerURLPolicy([]string{rawURL}); err == nil || !errors.Is(err, ErrInvalidTarget) {
				t.Fatalf("NewCustomServerURLPolicy(%q) error = %v", rawURL, err)
			}
		})
	}
}

func TestValidateHTTPURLUsesStrictSyntaxAndExplicitHTTPOptIn(t *testing.T) {
	if err := ValidateHTTPURL("https://speed.example.test/librespeed", false); err != nil {
		t.Fatalf("valid HTTPS rejected: %v", err)
	}
	if err := ValidateHTTPURL("http://speed.example.test/librespeed", false); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("HTTP without opt-in error = %v", err)
	}
	if err := ValidateHTTPURL("http://speed.example.test/librespeed", true); err != nil {
		t.Fatalf("opted-in HTTP rejected: %v", err)
	}
	for _, rawURL := range []string{
		"https://speed.example.test/librespeed?server=other",
		"https://speed.example.test/librespeed#other",
		"https://speed.example.test/%2e%2e/admin",
	} {
		if err := ValidateHTTPURL(rawURL, true); err == nil || !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("ValidateHTTPURL(%q) error = %v", rawURL, err)
		}
	}
}

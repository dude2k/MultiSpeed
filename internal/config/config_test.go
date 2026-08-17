package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadUsesManagedOoklaBinaryPathAndExplicitUploadOptIn(t *testing.T) {
	dataDirectory := t.TempDir()
	t.Setenv("APP_DATA_DIR", dataDirectory)
	t.Setenv("APP_ALLOW_OOKLA_BINARY_UPLOAD", "true")
	configuration, err := Load("test", "commit", "time")
	if err != nil {
		t.Fatal(err)
	}
	if configuration.OoklaBinary != filepath.Join(dataDirectory, "providers", "ookla", "speedtest") || !configuration.AllowOoklaBinaryUpload {
		t.Fatalf("unexpected Ookla upload config: path=%q enabled=%v", configuration.OoklaBinary, configuration.AllowOoklaBinaryUpload)
	}
}

func TestLoadRejectsInvalidBooleanEnvironmentValues(t *testing.T) {
	for _, key := range []string{"ACCEPT_OOKLA_EULA", "APP_ALLOW_OOKLA_BINARY_UPLOAD", "APP_METRICS_ENABLED"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "flase")
			if _, err := Load("test", "commit", "time"); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("Load() error=%v", err)
			}
		})
	}
}

func TestLoadTrimsBooleanEnvironmentValues(t *testing.T) {
	t.Setenv("ACCEPT_OOKLA_EULA", " true ")
	t.Setenv("APP_ALLOW_OOKLA_BINARY_UPLOAD", " false ")
	configuration, err := Load("test", "commit", "time")
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.AcceptOoklaEULA || configuration.AllowOoklaBinaryUpload {
		t.Fatalf("unexpected booleans: accept=%v upload=%v", configuration.AcceptOoklaEULA, configuration.AllowOoklaBinaryUpload)
	}
}

func TestLoadRejectsRelativeOrFilesystemRootDataDirectory(t *testing.T) {
	for _, dataDirectory := range []string{"relative-data", string(filepath.Separator)} {
		t.Run(dataDirectory, func(t *testing.T) {
			t.Setenv("APP_DATA_DIR", dataDirectory)
			if _, err := Load("test", "commit", "time"); err == nil || !strings.Contains(err.Error(), "absolute non-root path") {
				t.Fatalf("Load() error=%v", err)
			}
		})
	}
}

func TestParseTrustedHostsNormalizesAndDeduplicates(t *testing.T) {
	hosts, err := parseTrustedHosts(" dashboard.example.test,192.0.2.10,DASHBOARD.example.test,2001:db8::10 ")
	if err != nil {
		t.Fatalf("parse trusted hosts: %v", err)
	}
	want := []string{"dashboard.example.test", "192.0.2.10", "2001:db8::10"}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("trusted hosts = %#v, want %#v", hosts, want)
	}
}

func TestParseTrustedHostsRejectsUnsafeEntries(t *testing.T) {
	for _, value := range []string{
		"https://dashboard.example.test",
		"dashboard.example.test/path",
		"*.example.test",
		"dashboard.example.test:8787",
		"dashboard.example.test,,other.example.test",
		"bad_host.example.test",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseTrustedHosts(value); err == nil {
				t.Fatalf("parseTrustedHosts(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestLoadRejectsInvalidTrustedHosts(t *testing.T) {
	t.Setenv("APP_TRUSTED_HOSTS", "https://dashboard.example.test")
	if _, err := Load("test", "commit", "time"); err == nil {
		t.Fatal("Load unexpectedly accepted a trusted-host URL")
	}
}

func TestParseAllowedCustomServerURLsCanonicalizesAndDeduplicates(t *testing.T) {
	urls, err := parseAllowedCustomServerURLs(" HTTPS://Speed.Example.test:443/librespeed/,http://192.0.2.20:8080/speed,https://speed.example.test/librespeed ")
	if err != nil {
		t.Fatalf("parse allowed custom server URLs: %v", err)
	}
	want := []string{"http://192.0.2.20:8080/speed", "https://speed.example.test/librespeed"}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("allowed custom server URLs = %#v, want %#v", urls, want)
	}
	urls, err = parseAllowedCustomServerURLs("   ")
	if err != nil || urls != nil {
		t.Fatalf("empty allowlist = %#v, %v", urls, err)
	}
}

func TestParseAllowedCustomServerURLsRejectsUnsafeEntries(t *testing.T) {
	for _, value := range []string{
		"https://speed.example.test,",
		"https://user:secret@speed.example.test",
		"https://speed.example.test/path?query=yes",
		"https://speed.example.test/%2e%2e/admin",
		"https://[fe80::1%25eth0]/",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseAllowedCustomServerURLs(value); err == nil {
				t.Fatalf("parseAllowedCustomServerURLs(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestLoadParsesAllowedCustomServerURLs(t *testing.T) {
	t.Setenv("APP_ALLOWED_CUSTOM_SERVER_URLS", "https://Speed.Example.test:443/librespeed/")
	configuration, err := Load("test", "commit", "time")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"https://speed.example.test/librespeed"}
	if !reflect.DeepEqual(configuration.AllowedCustomServerURLs, want) {
		t.Fatalf("allowed custom server URLs = %#v, want %#v", configuration.AllowedCustomServerURLs, want)
	}
}

func TestLoadRejectsInvalidAllowedCustomServerURLs(t *testing.T) {
	t.Setenv("APP_ALLOWED_CUSTOM_SERVER_URLS", "https://speed.example.test/path?query=yes")
	if _, err := Load("test", "commit", "time"); err == nil || !strings.Contains(err.Error(), "APP_ALLOWED_CUSTOM_SERVER_URLS") {
		t.Fatalf("Load invalid allowlist error = %v", err)
	}
}

func TestValidateListenAddressRejectsMalformedHostsAndPorts(t *testing.T) {
	for _, address := range []string{"https://example.test:8787", "127.0.0.1:http", "127.0.0.1:0", "bad_host:8787"} {
		if err := validateListenAddress(address); err == nil {
			t.Errorf("validateListenAddress(%q) unexpectedly succeeded", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8787", "0.0.0.0:8787", "[::1]:8787", ":8787", "multispeed.local:8787"} {
		if err := validateListenAddress(address); err != nil {
			t.Errorf("validateListenAddress(%q): %v", address, err)
		}
	}
}

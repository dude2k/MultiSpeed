package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	ooklaprovider "github.com/dude2k/MultiSpeed/internal/providers/ookla"
)

func TestOoklaBinaryUploadRequiresEULAAndInstallsValidatedFile(t *testing.T) {
	handler, binaryPath := ooklaUploadTestHandler(t, true, true)
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/providers/ookla/binary", bytes.NewReader(apiTestAMD64ELF()))
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Origin", "http://multispeed.local")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result ooklaprovider.BinaryInstallResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.UploadEnabled || !result.Installed || result.Version != "Speedtest by Ookla 1.2.0.84" || len(result.SHA256) != 64 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if info, err := filepath.Glob(filepath.Join(filepath.Dir(binaryPath), ".speedtest-upload-*")); err != nil || len(info) != 0 {
		t.Fatalf("temporary files=%v error=%v", info, err)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/providers/ookla/binary", nil)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !bytes.Contains(statusResponse.Body.Bytes(), []byte(`"installed":true`)) {
		t.Fatalf("status response=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
}

func TestOoklaBinaryUploadFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		accepted    bool
		enabled     bool
		contentType string
		body        []byte
		length      int64
		wantStatus  int
		wantCode    string
	}{
		{name: "EULA required", enabled: true, contentType: "application/octet-stream", body: apiTestAMD64ELF(), wantStatus: 422, wantCode: "OOKLA_EULA_REQUIRED"},
		{name: "deployment opt-in required", accepted: true, contentType: "application/octet-stream", body: apiTestAMD64ELF(), wantStatus: 403, wantCode: "OOKLA_UPLOAD_DISABLED"},
		{name: "binary content type required", accepted: true, enabled: true, contentType: "application/json", body: apiTestAMD64ELF(), wantStatus: 415, wantCode: "UNSUPPORTED_CONTENT_TYPE"},
		{name: "invalid binary rejected", accepted: true, enabled: true, contentType: "application/octet-stream", body: []byte("not an ELF file"), wantStatus: 422, wantCode: "INVALID_OOKLA_BINARY"},
		{name: "oversized binary rejected", accepted: true, enabled: true, contentType: "application/octet-stream", body: []byte{0}, length: ooklaprovider.MaxBinaryUploadBytes + 1, wantStatus: 413, wantCode: "OOKLA_BINARY_TOO_LARGE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _ := ooklaUploadTestHandler(t, test.accepted, test.enabled)
			request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/providers/ookla/binary", bytes.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("Origin", "http://multispeed.local")
			if test.length > 0 {
				request.ContentLength = test.length
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || !bytes.Contains(response.Body.Bytes(), []byte(test.wantCode)) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func ooklaUploadTestHandler(t *testing.T, accepted, enabled bool) (http.Handler, string) {
	t.Helper()
	dataDirectory := t.TempDir()
	binaryPath := filepath.Join(dataDirectory, "providers", "ookla", "speedtest")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(dataDirectory, "api.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if accepted {
		if err := store.SetOoklaEULAAcceptance(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceService(broker)
	server := New(store, nil, nil, interfaces, network.NewRouteValidator(interfaces), providers.NewRegistry(), broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{
		ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local"}, DataDirectory: dataDirectory,
		OoklaBinaryPath: binaryPath, AllowOoklaBinaryUpload: enabled,
	})
	server.ooklaBinary = ooklaprovider.NewBinaryManager(dataDirectory, binaryPath, enabled, ooklaprovider.BinaryVerifierFunc(func(context.Context, string) (string, error) {
		return "Speedtest by Ookla 1.2.0.84", nil
	}))
	return server.Handler(), binaryPath
}

func apiTestAMD64ELF() []byte {
	header := make([]byte, 64)
	copy(header, []byte("\x7fELF"))
	header[4] = 2
	header[5] = 1
	header[6] = 1
	binary.LittleEndian.PutUint16(header[16:18], 3)
	binary.LittleEndian.PutUint16(header[18:20], 62)
	return header
}

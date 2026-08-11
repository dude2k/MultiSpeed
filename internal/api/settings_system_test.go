package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestOoklaEULAAcceptanceRequiresConfirmationAndPersistsSeparately(t *testing.T) {
	fixture := newBackendAPIFixture(t)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://multispeed.local/api/v1/settings/ookla-eula", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, req)
		return response
	}

	if response := request(`{"accepted":true,"confirmed":false}`); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "EULA_CONFIRMATION_REQUIRED") || !strings.Contains(response.Body.String(), "network access from multiple devices") {
		t.Fatalf("unconfirmed acceptance status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(`{"accepted":true,"confirmed":true,"unexpected":true}`); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}
	for _, incomplete := range []string{`{}`, `{"accepted":true}`, `{"confirmed":true}`} {
		if response := request(incomplete); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "EULA_REQUEST_INCOMPLETE") {
			t.Fatalf("incomplete request %s status=%d body=%s", incomplete, response.Code, response.Body.String())
		}
	}

	response := request(`{"accepted":true,"confirmed":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", response.Code, response.Body.String())
	}
	var settings models.Settings
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if !settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt == nil || settings.OoklaEULAVersion != models.CurrentOoklaEULAVersion || !settings.OoklaEULAEffectiveAccepted || settings.OoklaEULAAcceptanceSource != "persisted" {
		t.Fatalf("acceptance was not persisted: %+v", settings)
	}
	if response := request(`{}`); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty request after acceptance status=%d body=%s", response.Code, response.Body.String())
	}
	persisted, err := fixture.store.GetSettings(context.Background())
	if err != nil || !persisted.OoklaEULAAccepted {
		t.Fatalf("empty request changed persisted acceptance: %+v, %v", persisted, err)
	}

	// The general settings replacement cannot silently revoke consent.
	settings.OoklaEULAAccepted = false
	settings.OoklaEULAAcceptedAt = nil
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	generalRequest := httptest.NewRequest(http.MethodPut, "http://multispeed.local/api/v1/settings", strings.NewReader(string(body)))
	generalRequest.Header.Set("Content-Type", "application/json")
	generalResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(generalResponse, generalRequest)
	if generalResponse.Code != http.StatusOK {
		t.Fatalf("general update status=%d body=%s", generalResponse.Code, generalResponse.Body.String())
	}
	if err := json.Unmarshal(generalResponse.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if !settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt == nil {
		t.Fatalf("general settings update changed consent: %+v", settings)
	}

	response = request(`{"accepted":false,"confirmed":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt != nil {
		t.Fatalf("acceptance was not revoked: %+v", settings)
	}
}

func TestEffectiveOoklaEULAReportsEnvironmentOverride(t *testing.T) {
	server := &Server{ooklaEULAEnvironmentAccepted: true}
	settings := server.withEffectiveOoklaEULA(models.Settings{})
	if !settings.OoklaEULAEffectiveAccepted || settings.OoklaEULAAcceptanceSource != "environment" || settings.OoklaEULAAccepted {
		t.Fatalf("environment override was not represented accurately: %+v", settings)
	}
}

func TestValidateSettingsBoundsPersistedValues(t *testing.T) {
	valid := models.Settings{
		DisplayUnits: "bits", DefaultTimezone: "UTC", GlobalConcurrency: 1,
		RetentionMode: "forever", RetentionValue: 0, DefaultChartRange: "30d",
		InterfaceRefreshInterval: 30, DefaultTaskTimeout: 120, DatabaseMaintenanceSchedule: "0 3 * * 0",
	}
	if err := validateSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*models.Settings)
	}{
		{"display units length", func(value *models.Settings) { value.DisplayUnits = strings.Repeat("x", 9) }},
		{"timezone empty", func(value *models.Settings) { value.DefaultTimezone = "" }},
		{"timezone length", func(value *models.Settings) { value.DefaultTimezone = strings.Repeat("x", 129) }},
		{"retention mode length", func(value *models.Settings) { value.RetentionMode = strings.Repeat("x", 9) }},
		{"retention negative", func(value *models.Settings) { value.RetentionValue = -1 }},
		{"retention excessive", func(value *models.Settings) { value.RetentionValue = 3651 }},
		{"retention period zero", func(value *models.Settings) { value.RetentionMode, value.RetentionValue = "days", 0 }},
		{"chart range unknown", func(value *models.Settings) { value.DefaultChartRange = "1y" }},
		{"chart range excessive", func(value *models.Settings) { value.DefaultChartRange = strings.Repeat("x", 33) }},
		{"maintenance short", func(value *models.Settings) { value.DatabaseMaintenanceSchedule = "* * * *" }},
		{"maintenance length", func(value *models.Settings) { value.DatabaseMaintenanceSchedule = strings.Repeat("*", 257) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateSettings(value); err == nil {
				t.Fatalf("invalid settings unexpectedly accepted: %+v", value)
			}
		})
	}
}

func TestValidateSettingsAcceptsEveryChartRangeAndRetentionUpperBound(t *testing.T) {
	for _, chartRange := range []string{"24h", "7d", "30d", "90d"} {
		value := models.Settings{
			DisplayUnits: "bytes", DefaultTimezone: "Europe/Berlin", GlobalConcurrency: 16,
			RetentionMode: "months", RetentionValue: 3650, DefaultChartRange: chartRange,
			InterfaceRefreshInterval: 3600, DefaultTaskTimeout: 3600, DatabaseMaintenanceSchedule: "30 3 * * *",
		}
		if err := validateSettings(value); err != nil {
			t.Errorf("chart range %q rejected: %v", chartRange, err)
		}
	}
}

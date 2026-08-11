package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/retention"
	"github.com/robfig/cron/v3"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.build.Version, "uptimeSeconds": int64(time.Since(s.startedAt).Seconds())})
}
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "NOT_READY", "Database readiness check failed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withEffectiveOoklaEULA(settings))
}
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	var settings models.Settings
	if !decodeJSON(w, r, &settings) {
		return
	}
	if err := validateSettings(settings); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_SETTINGS", err.Error())
		return
	}
	if err := s.store.UpdateSettings(r.Context(), settings); err != nil {
		handleStoreError(w, r, err)
		return
	}
	updated, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	updated = s.withEffectiveOoklaEULA(updated)
	s.broker.Publish("settings.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) updateOoklaEULA(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Accepted  *bool `json:"accepted"`
		Confirmed *bool `json:"confirmed"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Accepted == nil || request.Confirmed == nil {
		writeError(w, r, http.StatusUnprocessableEntity, "EULA_REQUEST_INCOMPLETE", "Both accepted and confirmed are required by this legacy-named technical acknowledgement endpoint.")
		return
	}
	if *request.Accepted && !*request.Confirmed {
		writeError(w, r, http.StatusUnprocessableEntity, "EULA_CONFIRMATION_REQUIRED", "Explicit agreement to the current Ookla EULA and Terms of Use, review of its Privacy Policy, authorization for --accept-license and --accept-gdpr, and confirmation that the deployment, device type, and any network access from multiple devices fit the express personal-computer/non-commercial grant or have separate written Ookla authorization are required.")
		return
	}
	if err := s.store.SetOoklaEULAAcceptance(r.Context(), *request.Accepted); err != nil {
		handleStoreError(w, r, err)
		return
	}
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	settings = s.withEffectiveOoklaEULA(settings)
	s.broker.Publish("settings.updated", settings)
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) withEffectiveOoklaEULA(settings models.Settings) models.Settings {
	settings.OoklaEULACurrentVersion = models.CurrentOoklaEULAVersion
	if s.ooklaEULAEnvironmentAccepted {
		settings.OoklaEULAEffectiveAccepted = true
		settings.OoklaEULAAcceptanceSource = "environment"
		return settings
	}
	settings.OoklaEULAEffectiveAccepted = settings.OoklaEULAAccepted
	if settings.OoklaEULAAccepted {
		settings.OoklaEULAAcceptanceSource = "persisted"
	} else {
		settings.OoklaEULAAcceptanceSource = "none"
	}
	return settings
}
func validateSettings(value models.Settings) error {
	if len(value.DisplayUnits) < 1 || len(value.DisplayUnits) > 8 {
		return fmt.Errorf("display units exceed their size limit")
	}
	if value.DisplayUnits != "bits" && value.DisplayUnits != "bytes" {
		return fmt.Errorf("display units must be bits or bytes")
	}
	if len(value.DefaultTimezone) < 1 || len(value.DefaultTimezone) > 128 {
		return fmt.Errorf("default timezone must contain between 1 and 128 characters")
	}
	if _, err := time.LoadLocation(value.DefaultTimezone); err != nil {
		return fmt.Errorf("default timezone: %w", err)
	}
	if value.GlobalConcurrency < 1 || value.GlobalConcurrency > 16 {
		return fmt.Errorf("global concurrency must be between 1 and 16")
	}
	if len(value.RetentionMode) < 1 || len(value.RetentionMode) > 8 || value.RetentionMode != "forever" && value.RetentionMode != "days" && value.RetentionMode != "months" {
		return fmt.Errorf("retention mode must be forever, days, or months")
	}
	if value.RetentionValue < 0 || value.RetentionValue > 3650 {
		return fmt.Errorf("retention value must be between 0 and 3650")
	}
	if value.RetentionMode != "forever" && value.RetentionValue < 1 {
		return fmt.Errorf("retention value must be positive")
	}
	if value.DefaultChartRange != "24h" && value.DefaultChartRange != "7d" && value.DefaultChartRange != "30d" && value.DefaultChartRange != "90d" {
		return fmt.Errorf("default chart range must be 24h, 7d, 30d, or 90d")
	}
	if value.InterfaceRefreshInterval < 5 || value.InterfaceRefreshInterval > 3600 {
		return fmt.Errorf("interface refresh interval must be between 5 and 3600 seconds")
	}
	if value.DefaultTaskTimeout < 5 || value.DefaultTaskTimeout > 3600 {
		return fmt.Errorf("default task timeout must be between 5 and 3600 seconds")
	}
	if len(value.DatabaseMaintenanceSchedule) < 9 || len(value.DatabaseMaintenanceSchedule) > 256 {
		return fmt.Errorf("database maintenance schedule must contain between 9 and 256 characters")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(value.DatabaseMaintenanceSchedule); err != nil {
		return fmt.Errorf("database maintenance schedule: %w", err)
	}
	return nil
}

func (s *Server) cleanupResults(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Before *time.Time `json:"before"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &request) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	var metrics retention.Metrics
	var err error
	if request.Before != nil {
		if !request.Before.Before(time.Now()) {
			writeError(w, r, http.StatusUnprocessableEntity, "INVALID_CUTOFF", "The cleanup cutoff must be in the past.")
			return
		}
		metrics, err = s.retentionCleaner.RunBefore(ctx, request.Before.UTC())
	} else {
		settings, settingsErr := s.store.GetSettings(ctx)
		if settingsErr != nil {
			handleStoreError(w, r, settingsErr)
			return
		}
		metrics, err = s.retentionCleaner.Run(ctx, retention.Policy{Mode: retention.Mode(settings.RetentionMode), Value: settings.RetentionValue}, time.Now().UTC())
	}
	if err != nil {
		s.logger.Error("retention cleanup failed", "request_id", requestIDFrom(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "RETENTION_FAILED", "Result cleanup could not be completed.")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	schemaVersion, err := s.store.SchemaVersion(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	taskCount, resultCount, running, err := s.store.Counts(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	interfaces, refreshedAt := s.interfaces.Snapshot(true, true, true)
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{"product": "MultiSpeed", "version": s.build.Version, "gitCommit": s.build.GitCommit, "buildTime": s.build.BuildTime, "buildDate": s.build.BuildTime, "goVersion": runtime.Version(), "operatingSystem": runtime.GOOS, "architecture": runtime.GOARCH, "hostname": host, "databasePath": s.store.Path(), "databaseSizeBytes": s.store.DatabaseSize(), "schemaVersion": schemaVersion, "providers": s.providers.Descriptors(r.Context()), "interfaces": interfaces, "interfacesRefreshedAt": refreshedAt, "taskCount": taskCount, "resultCount": resultCount, "runningTaskCount": running, "uptimeSeconds": int64(time.Since(s.startedAt).Seconds()), "authenticationEnabled": false, "internetExposureWarning": "MultiSpeed does not include authentication and must only be exposed to trusted networks unless protected by an authenticating reverse proxy."})
}

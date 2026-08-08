package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/google/uuid"
)

type configurationDocumentRequest struct {
	models.ConfigurationDocument
}

func (request *configurationDocumentRequest) UnmarshalJSON(data []byte) error {
	if err := validateConfigurationRequiredFields(data); err != nil {
		return err
	}
	type documentAlias models.ConfigurationDocument
	var document documentAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("only one configuration document is allowed")
	}
	request.ConfigurationDocument = models.ConfigurationDocument(document)
	return nil
}

func (s *Server) exportConfiguration(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.Configuration(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	document := models.ConfigurationDocument{
		Format: models.ConfigurationFormat, Version: models.ConfigurationFormatVersion,
		ExportedAt: time.Now().UTC(), ApplicationVersion: s.build.Version,
		Settings:      models.ConfigurationSettingsFrom(snapshot.Settings),
		RouteProfiles: make([]models.ConfigurationRouteProfile, len(snapshot.RouteProfiles)),
		Tasks:         make([]models.ConfigurationTask, len(snapshot.Tasks)),
	}
	for index := range snapshot.RouteProfiles {
		document.RouteProfiles[index] = models.ConfigurationRouteProfileFrom(snapshot.RouteProfiles[index])
	}
	for index := range snapshot.Tasks {
		document.Tasks[index] = models.ConfigurationTaskFrom(snapshot.Tasks[index])
	}
	filename := "multispeed-config-" + document.ExportedAt.Format("20060102T150405Z") + ".json"
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	writeJSON(w, http.StatusOK, document)
}

func (s *Server) importConfiguration(w http.ResponseWriter, r *http.Request) {
	var request configurationDocumentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	document := request.ConfigurationDocument
	settings, routes, tasks, err := s.validateConfigurationDocument(r.Context(), document)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_CONFIGURATION", err.Error())
		return
	}

	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	current, err := s.store.Configuration(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	if s.scheduler != nil {
		if err := s.scheduler.Reconcile(r.Context(), nil); err != nil {
			writeError(w, r, http.StatusInternalServerError, "CONFIGURATION_IMPORT_FAILED", "Scheduled tasks could not be paused for import.")
			return
		}
	}
	if err := s.store.ReplaceConfiguration(r.Context(), settings, routes, tasks); err != nil {
		if s.scheduler != nil {
			_ = s.scheduler.Reconcile(r.Context(), current.Tasks)
		}
		if errors.Is(err, database.ErrActive) {
			writeError(w, r, http.StatusConflict, "CONFIGURATION_IMPORT_ACTIVE", "Configuration cannot be imported while a test is queued, validating, or running.")
			return
		}
		handleStoreError(w, r, err)
		return
	}
	if s.scheduler != nil {
		if err := s.scheduler.Reconcile(r.Context(), tasks); err != nil {
			s.logger.ErrorContext(r.Context(), "reconcile imported configuration", "request_id", requestIDFrom(r.Context()), "error", err)
			writeError(w, r, http.StatusInternalServerError, "CONFIGURATION_IMPORT_FAILED", "The configuration was stored, but its schedules could not be activated. Restart MultiSpeed before running tasks.")
			return
		}
	}
	result := models.ConfigurationImportResult{
		ImportedAt: time.Now().UTC(), TaskCount: len(tasks), RouteProfileCount: len(routes), SettingsUpdated: true,
	}
	s.broker.Publish("configuration.imported", result)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) validateConfigurationDocument(ctx context.Context, document models.ConfigurationDocument) (models.Settings, []models.RouteProfile, []models.Task, error) {
	if document.Format != models.ConfigurationFormat {
		return models.Settings{}, nil, nil, fmt.Errorf("format must be %q", models.ConfigurationFormat)
	}
	if document.Version != models.ConfigurationFormatVersion {
		return models.Settings{}, nil, nil, fmt.Errorf("configuration version %d is not supported; this release accepts version %d", document.Version, models.ConfigurationFormatVersion)
	}
	if document.ExportedAt.IsZero() {
		return models.Settings{}, nil, nil, fmt.Errorf("exportedAt is required")
	}
	if len(document.ApplicationVersion) > 128 {
		return models.Settings{}, nil, nil, fmt.Errorf("applicationVersion exceeds 128 characters")
	}
	if len(document.RouteProfiles) > 1000 || len(document.Tasks) > 1000 {
		return models.Settings{}, nil, nil, fmt.Errorf("configuration may contain at most 1000 tasks and 1000 route profiles")
	}
	if document.RouteProfiles == nil || document.Tasks == nil {
		return models.Settings{}, nil, nil, fmt.Errorf("tasks and routeProfiles must be JSON arrays")
	}

	settings := document.Settings.Model()
	if err := validateSettings(settings); err != nil {
		return models.Settings{}, nil, nil, fmt.Errorf("settings: %w", err)
	}
	routes := make([]models.RouteProfile, len(document.RouteProfiles))
	routeByID := make(map[string]models.RouteProfile, len(routes))
	for index := range document.RouteProfiles {
		profile := document.RouteProfiles[index].Model()
		applyRouteDefaults(&profile)
		if err := validateConfigurationID("route profile", profile.ID); err != nil {
			return models.Settings{}, nil, nil, err
		}
		if _, duplicate := routeByID[profile.ID]; duplicate {
			return models.Settings{}, nil, nil, fmt.Errorf("route profile ID %q occurs more than once", profile.ID)
		}
		if err := validateRouteFields(&profile); err != nil {
			return models.Settings{}, nil, nil, fmt.Errorf("route profile %q: %w", profile.Name, err)
		}
		routes[index] = profile
		routeByID[profile.ID] = profile
	}

	tasks := make([]models.Task, len(document.Tasks))
	taskIDs := make(map[string]struct{}, len(tasks))
	for index := range document.Tasks {
		task := document.Tasks[index].Model()
		applyTaskDefaults(&task, settings)
		if err := validateConfigurationID("task", task.ID); err != nil {
			return models.Settings{}, nil, nil, err
		}
		if _, duplicate := taskIDs[task.ID]; duplicate {
			return models.Settings{}, nil, nil, fmt.Errorf("task ID %q occurs more than once", task.ID)
		}
		taskIDs[task.ID] = struct{}{}
		ip, err := validateTaskFields(&task)
		if err != nil {
			return models.Settings{}, nil, nil, fmt.Errorf("task %q: %w", task.Name, err)
		}
		if s.scheduler == nil {
			return models.Settings{}, nil, nil, fmt.Errorf("task scheduler is unavailable")
		}
		if err := s.validateTaskTargetAndSchedule(ctx, &task); err != nil {
			return models.Settings{}, nil, nil, fmt.Errorf("task %q: %w", task.Name, err)
		}
		if task.Enabled {
			if err := s.interfaces.ValidateSource(task.InterfaceName, task.SourceIP); err != nil {
				return models.Settings{}, nil, nil, fmt.Errorf("enabled task %q: %w", task.Name, err)
			}
		}
		if task.RouteProfileID != nil {
			profile, present := routeByID[*task.RouteProfileID]
			if !present {
				return models.Settings{}, nil, nil, fmt.Errorf("task %q references route profile %q, which is not included", task.Name, *task.RouteProfileID)
			}
			if profile.InterfaceName != task.InterfaceName || !net.ParseIP(profile.SourceIP).Equal(ip) {
				return models.Settings{}, nil, nil, fmt.Errorf("task %q and its route profile must use the same interface and source IP", task.Name)
			}
		}
		tasks[index] = task
	}
	return settings, routes, tasks, nil
}

func validateConfigurationID(kind, id string) error {
	if strings.TrimSpace(id) != id || uuid.Validate(id) != nil {
		return fmt.Errorf("%s ID %q must be a valid UUID", kind, id)
	}
	return nil
}

func validateConfigurationRequiredFields(data []byte) error {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := requireJSONFields(document, "configuration", []string{"format", "version", "exportedAt", "applicationVersion", "settings", "routeProfiles", "tasks"}, nil); err != nil {
		return err
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(document["settings"], &settings); err != nil {
		return fmt.Errorf("settings must be an object: %w", err)
	}
	if err := requireJSONFields(settings, "settings", []string{"displayUnits", "defaultTimezone", "globalConcurrency", "allowSeparateWanConcurrency", "retentionMode", "retentionValue", "defaultChartRange", "interfaceRefreshIntervalSeconds", "defaultTaskTimeoutSeconds", "databaseMaintenanceSchedule"}, nil); err != nil {
		return err
	}
	if err := validateConfigurationArrayFields(document["routeProfiles"], "route profile", []string{"id", "name", "description", "interfaceName", "sourceIp", "expectedGateway", "expectedRoutingTable", "validationTarget", "notes"}, nil); err != nil {
		return err
	}
	return validateConfigurationArrayFields(document["tasks"], "task", []string{"id", "name", "description", "enabled", "provider", "cronExpression", "timezone", "randomJitterSeconds", "serverSelectionMode", "serverId", "serverUrl", "customServerDefinition", "interfaceName", "sourceIp", "ipFamily", "routeProfileId", "timeoutSeconds", "providerOptions", "preventOverlap", "routeValidation"}, map[string]bool{"routeProfileId": true})
}

func validateConfigurationArrayFields(raw json.RawMessage, kind string, required []string, nullable map[string]bool) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		if err == nil {
			err = fmt.Errorf("value is null")
		}
		return fmt.Errorf("%ss must be an array: %w", kind, err)
	}
	for index := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(items[index], &item); err != nil {
			return fmt.Errorf("%s %d must be an object: %w", kind, index+1, err)
		}
		if err := requireJSONFields(item, fmt.Sprintf("%s %d", kind, index+1), required, nullable); err != nil {
			return err
		}
	}
	return nil
}

func requireJSONFields(value map[string]json.RawMessage, label string, required []string, nullable map[string]bool) error {
	if value == nil {
		return fmt.Errorf("%s must be an object", label)
	}
	for _, field := range required {
		raw, present := value[field]
		if !present || (!nullable[field] && bytes.Equal(bytes.TrimSpace(raw), []byte("null"))) {
			return fmt.Errorf("%s field %q is required", label, field)
		}
	}
	return nil
}

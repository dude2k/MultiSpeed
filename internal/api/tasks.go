package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/go-chi/chi/v5"
)

type taskInput struct {
	Name                   string                 `json:"name"`
	Description            *string                `json:"description"`
	Enabled                bool                   `json:"enabled"`
	Provider               models.ProviderID      `json:"provider"`
	CronExpression         *string                `json:"cronExpression"`
	Timezone               *string                `json:"timezone"`
	RandomJitterSeconds    *int                   `json:"randomJitterSeconds"`
	ServerSelectionMode    *string                `json:"serverSelectionMode"`
	ServerID               *string                `json:"serverId"`
	ServerURL              *string                `json:"serverUrl"`
	CustomServerDefinition map[string]any         `json:"customServerDefinition"`
	InterfaceName          string                 `json:"interfaceName"`
	SourceIP               string                 `json:"sourceIp"`
	IPFamily               *string                `json:"ipFamily"`
	RouteProfileID         optionalNullableString `json:"routeProfileId"`
	TimeoutSeconds         *int                   `json:"timeoutSeconds"`
	ProviderOptions        map[string]any         `json:"providerOptions"`
	PreventOverlap         *bool                  `json:"preventOverlap"`
	RouteValidation        *string                `json:"routeValidation"`
}

type optionalNullableString struct {
	present bool
	value   *string
}

func (value *optionalNullableString) UnmarshalJSON(data []byte) error {
	value.present = true
	if string(data) == "null" {
		value.value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("must be a string or null: %w", err)
	}
	value.value = &decoded
	return nil
}

func (input taskInput) model(existing *models.Task) models.Task {
	preventOverlap := true
	description, serverID, serverURL := "", "", ""
	cronExpression, timezone, serverSelectionMode := "", "", ""
	ipFamily, routeValidation, timeoutSeconds, randomJitterSeconds := "", "", 0, 0
	var routeProfileID *string
	customServerDefinition := input.CustomServerDefinition
	providerOptions := input.ProviderOptions
	if existing != nil {
		preventOverlap = existing.PreventOverlap
		description, serverID, serverURL = existing.Description, existing.ServerID, existing.ServerURL
		cronExpression, timezone = existing.CronExpression, existing.Timezone
		serverSelectionMode, ipFamily = existing.ServerSelectionMode, existing.IPFamily
		routeValidation, timeoutSeconds = existing.RouteValidation, existing.TimeoutSeconds
		randomJitterSeconds = existing.RandomJitterSeconds
		routeProfileID = existing.RouteProfileID
		if customServerDefinition == nil {
			customServerDefinition = existing.CustomServerDefinition
		}
		if providerOptions == nil {
			providerOptions = existing.ProviderOptions
		}
	}
	if input.PreventOverlap != nil {
		preventOverlap = *input.PreventOverlap
	}
	if input.Description != nil {
		description = *input.Description
	}
	if input.CronExpression != nil {
		cronExpression = *input.CronExpression
	}
	if input.Timezone != nil {
		timezone = *input.Timezone
	}
	if input.RandomJitterSeconds != nil {
		randomJitterSeconds = *input.RandomJitterSeconds
	}
	if input.ServerSelectionMode != nil {
		serverSelectionMode = *input.ServerSelectionMode
	}
	if input.IPFamily != nil {
		ipFamily = *input.IPFamily
	}
	if input.ServerID != nil {
		serverID = *input.ServerID
	}
	if input.ServerURL != nil {
		serverURL = *input.ServerURL
	}
	if input.RouteProfileID.present {
		routeProfileID = input.RouteProfileID.value
	}
	if input.TimeoutSeconds != nil {
		timeoutSeconds = *input.TimeoutSeconds
	}
	if input.RouteValidation != nil {
		routeValidation = *input.RouteValidation
	}
	return models.Task{Name: input.Name, Description: description, Enabled: input.Enabled, Provider: input.Provider,
		CronExpression: cronExpression, Timezone: timezone, RandomJitterSeconds: randomJitterSeconds,
		ServerSelectionMode: serverSelectionMode, ServerID: serverID, ServerURL: serverURL,
		CustomServerDefinition: customServerDefinition, InterfaceName: input.InterfaceName, SourceIP: input.SourceIP,
		IPFamily: ipFamily, RouteProfileID: routeProfileID, TimeoutSeconds: timeoutSeconds,
		ProviderOptions: providerOptions, PreventOverlap: preventOverlap, RouteValidation: routeValidation}
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTasks(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	for index := range items {
		s.decorateTaskNetworkState(&items[index])
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.decorateTaskNetworkState(&item)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	task := input.model(nil)
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	applyTaskDefaults(&task, settings)
	if err := s.validateTaskModel(r.Context(), &task); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_TASK", err.Error())
		return
	}
	if task.Enabled && !s.requireEnabledTaskPreflight(w, r, task) {
		return
	}
	if err := s.store.CreateTask(r.Context(), &task); err != nil {
		handleStoreError(w, r, err)
		return
	}
	if err := s.scheduler.Reschedule(r.Context(), task); err != nil {
		_ = s.store.DeleteTask(r.Context(), task.ID)
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_SCHEDULE", err.Error())
		return
	}
	task, _ = s.store.GetTask(r.Context(), task.ID)
	s.decorateTaskNetworkState(&task)
	s.broker.Publish("task.created", task)
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	task := input.model(&existing)
	task.ID = id
	task.CreatedAt = existing.CreatedAt
	task.LastScheduledAt = existing.LastScheduledAt
	task.NextScheduledAt = existing.NextScheduledAt
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	applyTaskDefaults(&task, settings)
	if err := s.validateTaskModel(r.Context(), &task); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_TASK", err.Error())
		return
	}
	// Validate every resulting enabled configuration before persistence. This
	// covers disabled-to-enabled transitions as well as edits to enabled tasks,
	// while intentionally allowing unavailable-provider drafts to stay disabled.
	if task.Enabled && !s.requireEnabledTaskPreflight(w, r, task) {
		return
	}
	if err := s.store.UpdateTask(r.Context(), &task); err != nil {
		handleStoreError(w, r, err)
		return
	}
	if err := s.scheduler.Reschedule(r.Context(), task); err != nil {
		_ = s.store.UpdateTask(r.Context(), &existing)
		_ = s.scheduler.Reschedule(r.Context(), existing)
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_SCHEDULE", err.Error())
		return
	}
	task, _ = s.store.GetTask(r.Context(), id)
	s.decorateTaskNetworkState(&task)
	event := "task.updated"
	if existing.Enabled && !task.Enabled {
		event = "task.disabled"
	}
	if !existing.Enabled && task.Enabled {
		event = "task.enabled"
	}
	s.broker.Publish(event, task)
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	if err := s.scheduler.Remove(r.Context(), id); err != nil {
		handleStoreError(w, r, err)
		return
	}
	if err := s.store.DeleteTask(r.Context(), id); err != nil {
		_ = s.scheduler.Reschedule(r.Context(), existing)
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("task.deleted", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) duplicateTask(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	source, err := s.store.GetTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	source.ID = ""
	source.Name = truncateString(source.Name+" copy", 120)
	source.Enabled = false
	source.CreatedAt = time.Time{}
	source.UpdatedAt = time.Time{}
	source.LastScheduledAt = nil
	source.NextScheduledAt = nil
	if err := s.store.CreateTask(r.Context(), &source); err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("task.created", source)
	s.decorateTaskNetworkState(&source)
	writeJSON(w, http.StatusCreated, source)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if !s.manualLimit.Allow(r) {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Manual execution is limited to four requests per minute per client.")
		return
	}
	result, err := s.execution.Queue(r.Context(), chi.URLParam(r, "id"), models.TriggerManual, nil)
	if err != nil {
		if err == database.ErrNotFound {
			handleStoreError(w, r, err)
			return
		}
		writeError(w, r, http.StatusConflict, "RUN_NOT_QUEUED", providers.SanitizeOutput(err.Error(), 1024))
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) validateTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	if err := s.validateTaskModel(r.Context(), &task); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "TASK_VALIDATION_FAILED", err.Error())
		return
	}
	validation, err := s.preflightTaskPath(r.Context(), task)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "TASK_VALIDATION_FAILED", providerErrorMessage(err, "The provider target preflight failed."))
		return
	}
	if task.RouteProfileID != nil {
		_ = s.store.UpdateRouteValidation(r.Context(), *task.RouteProfileID, validation)
	}
	status := http.StatusOK
	if !validation.Success {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, validation)
}

// validateTransientTask performs the same model, provider, interface, route,
// and public-path checks as a saved task without creating or executing one.
func (s *Server) validateTransientTask(w http.ResponseWriter, r *http.Request) {
	var input taskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	task := input.model(nil)
	applyTaskDefaults(&task, settings)
	if err := s.validateTaskModel(r.Context(), &task); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "TASK_VALIDATION_FAILED", err.Error())
		return
	}
	validation, err := s.preflightTaskPath(r.Context(), task)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "TASK_VALIDATION_FAILED", providerErrorMessage(err, "The provider target preflight failed."))
		return
	}
	status := http.StatusOK
	if !validation.Success {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, validation)
}

func (s *Server) requireEnabledTaskPreflight(w http.ResponseWriter, r *http.Request, task models.Task) bool {
	validation, err := s.preflightTaskPath(r.Context(), task)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "TASK_PREFLIGHT_FAILED", providerErrorMessage(err, "The enabled task preflight failed."))
		return false
	}
	if validation.Success {
		return true
	}
	message := providers.SanitizeOutput(validation.Message, providerErrorLimit)
	if message == "" {
		message = "The selected task route could not be validated."
	}
	writeError(w, r, http.StatusUnprocessableEntity, "TASK_PREFLIGHT_FAILED", message)
	return false
}

func (s *Server) preflightTaskPath(ctx context.Context, task models.Task) (models.RouteValidation, error) {
	providerContext, cancelProvider := context.WithTimeout(ctx, providerRequestTimeout)
	err := s.preflightTaskProviderTarget(providerContext, task)
	cancelProvider()
	if err != nil {
		return models.RouteValidation{}, err
	}
	profile, err := s.effectiveRouteProfile(ctx, task)
	if err != nil {
		return models.RouteValidation{}, err
	}
	validationContext, cancelValidation := context.WithTimeout(ctx, 45*time.Second)
	validation := s.routes.Validate(validationContext, profile)
	cancelValidation()
	return validation, nil
}

func (s *Server) preflightTaskProviderTarget(ctx context.Context, task models.Task) error {
	provider, err := s.providers.Get(task.Provider)
	if err != nil {
		return err
	}
	return s.preflightProviderTarget(ctx, provider, providerServerValidationInput{
		SelectionMode:          task.ServerSelectionMode,
		ServerID:               task.ServerID,
		ServerURL:              task.ServerURL,
		CustomServerDefinition: task.CustomServerDefinition,
		ProviderOptions:        task.ProviderOptions,
		InterfaceName:          task.InterfaceName,
		SourceIP:               task.SourceIP,
		IPFamily:               task.IPFamily,
	})
}

func (s *Server) effectiveRouteProfile(ctx context.Context, task models.Task) (models.RouteProfile, error) {
	var storedProfile *models.RouteProfile
	if task.RouteProfileID != nil {
		profile, err := s.store.GetRouteProfile(ctx, *task.RouteProfileID)
		if err != nil {
			return models.RouteProfile{}, err
		}
		storedProfile = &profile
	}
	return execution.RouteProfileForTask(task, storedProfile)
}

func (s *Server) nextRuns(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	runs, err := s.scheduler.NextRuns(task, 5)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_SCHEDULE", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": task.ID, "cronExpression": task.CronExpression, "timezone": task.Timezone, "nextRuns": runs})
}

func applyTaskDefaults(task *models.Task, settings models.Settings) {
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.InterfaceName = strings.TrimSpace(task.InterfaceName)
	task.SourceIP = strings.TrimSpace(task.SourceIP)
	task.ServerID = strings.TrimSpace(task.ServerID)
	task.ServerURL = strings.TrimSpace(task.ServerURL)
	if task.CronExpression == "" {
		task.CronExpression = "0 * * * *"
	}
	if task.Timezone == "" {
		task.Timezone = settings.DefaultTimezone
	}
	if task.ServerSelectionMode == "" {
		task.ServerSelectionMode = "automatic"
	}
	if task.IPFamily == "" {
		task.IPFamily = "auto"
	}
	if task.TimeoutSeconds == 0 {
		task.TimeoutSeconds = settings.DefaultTaskTimeout
	}
	if task.RouteValidation == "" {
		task.RouteValidation = "required"
	}
	if task.CustomServerDefinition == nil {
		task.CustomServerDefinition = map[string]any{}
	}
	if task.ProviderOptions == nil {
		task.ProviderOptions = map[string]any{}
	}
}

func (s *Server) validateTaskModel(ctx context.Context, task *models.Task) error {
	ip, err := validateTaskFields(task)
	if err != nil {
		return err
	}
	if err := s.interfaces.ValidateSource(task.InterfaceName, task.SourceIP); err != nil {
		return err
	}
	if err := s.validateTaskTargetAndSchedule(ctx, task); err != nil {
		return err
	}
	if task.RouteProfileID != nil {
		profile, err := s.store.GetRouteProfile(ctx, *task.RouteProfileID)
		if err != nil {
			return fmt.Errorf("route profile: %w", err)
		}
		if profile.InterfaceName != task.InterfaceName || !net.ParseIP(profile.SourceIP).Equal(ip) {
			return fmt.Errorf("route profile must use the same interface and source IP as the task")
		}
	}
	return nil
}

func validateTaskFields(task *models.Task) (net.IP, error) {
	if len(task.Name) < 1 || len(task.Name) > 120 {
		return nil, fmt.Errorf("name must contain between 1 and 120 characters")
	}
	if len(task.Description) > 4000 {
		return nil, fmt.Errorf("description must not exceed 4000 characters")
	}
	if err := validateInterfaceName(task.InterfaceName); err != nil {
		return nil, err
	}
	if task.RandomJitterSeconds < 0 || task.RandomJitterSeconds > 3600 {
		return nil, fmt.Errorf("random start jitter must be between 0 and 3600 seconds")
	}
	if task.TimeoutSeconds < 5 || task.TimeoutSeconds > 3600 {
		return nil, fmt.Errorf("timeout must be between 5 and 3600 seconds")
	}
	if task.IPFamily != "auto" && task.IPFamily != "ipv4" && task.IPFamily != "ipv6" {
		return nil, fmt.Errorf("IP family must be auto, ipv4, or ipv6")
	}
	if task.RouteValidation != "required" && task.RouteValidation != "interface-only" {
		return nil, fmt.Errorf("route validation must be required or interface-only")
	}
	ip := net.ParseIP(task.SourceIP)
	if ip == nil {
		return nil, fmt.Errorf("source IP is invalid")
	}
	if task.IPFamily == "ipv4" && ip.To4() == nil {
		return nil, fmt.Errorf("source IP is not IPv4")
	}
	if task.IPFamily == "ipv6" && ip.To4() != nil {
		return nil, fmt.Errorf("source IP is not IPv6")
	}
	// Persist one canonical spelling so equivalent IPv6 forms share the same
	// network-path identity throughout scheduling and execution.
	task.SourceIP = ip.String()
	return ip, nil
}

func (s *Server) validateTaskTargetAndSchedule(ctx context.Context, task *models.Task) error {
	if _, err := s.scheduler.NextRuns(*task, 5); err != nil {
		return err
	}
	provider, err := s.providers.Get(task.Provider)
	if err != nil {
		return err
	}
	if err := provider.Validate(ctx, providers.TestTarget{SelectionMode: task.ServerSelectionMode, ServerID: task.ServerID, ServerURL: task.ServerURL, CustomServerDefinition: task.CustomServerDefinition}); err != nil {
		return err
	}
	return nil
}

func validationTarget(source string) string {
	ip := net.ParseIP(source)
	if ip != nil && ip.To4() == nil {
		return "2606:4700:4700::1111"
	}
	return "1.1.1.1"
}
func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func (s *Server) decorateTaskNetworkState(task *models.Task) {
	if err := s.interfaces.ValidateSource(task.InterfaceName, task.SourceIP); err != nil {
		task.NetworkPathValid = false
		task.NetworkPathMessage = err.Error()
		return
	}
	task.NetworkPathValid = true
	task.NetworkPathMessage = "The selected source address is present on the interface."
}

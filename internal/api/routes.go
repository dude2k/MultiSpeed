package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/go-chi/chi/v5"
)

var (
	routingTableNumberPattern = regexp.MustCompile(`^[0-9]{1,10}$`)
	routingTableNamePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
)

type routeProfileInput struct {
	Name                 string `json:"name"`
	Description          string `json:"description"`
	InterfaceName        string `json:"interfaceName"`
	SourceIP             string `json:"sourceIp"`
	ExpectedGateway      string `json:"expectedGateway"`
	ExpectedRoutingTable string `json:"expectedRoutingTable"`
	ValidationTarget     string `json:"validationTarget"`
	Notes                string `json:"notes"`
}

func (input routeProfileInput) model() models.RouteProfile {
	return models.RouteProfile{Name: input.Name, Description: input.Description, InterfaceName: input.InterfaceName,
		SourceIP: input.SourceIP, ExpectedGateway: input.ExpectedGateway, ExpectedRoutingTable: input.ExpectedRoutingTable,
		ValidationTarget: input.ValidationTarget, Notes: input.Notes, LastValidationSnapshot: map[string]any{}}
}

func (s *Server) listInterfaces(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	loopback, err := strictQueryBoolean(query.Get("includeLoopback"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", "includeLoopback must be true or false")
		return
	}
	down, err := strictQueryBoolean(query.Get("includeDown"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", "includeDown must be true or false")
		return
	}
	virtual, err := strictQueryBoolean(query.Get("includeVirtual"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", "includeVirtual must be true or false")
		return
	}
	items, refreshed := s.interfaces.Snapshot(loopback, down, virtual)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "refreshedAt": refreshed})
}

func strictQueryBoolean(value string) (bool, error) {
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("must be true or false")
	}
}
func (s *Server) refreshInterfaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.interfaces.Refresh(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "interface refresh failed", "request_id", requestIDFrom(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "INTERFACE_REFRESH_FAILED", "The network-interface snapshot could not be refreshed.")
		return
	}
	_, refreshedAt := s.interfaces.Snapshot(true, true, true)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "interfaces": items, "refreshedAt": refreshedAt})
}

func (s *Server) listRouteProfiles(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRouteProfiles(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) getRouteProfile(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRouteProfile(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) createRouteProfile(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	var input routeProfileInput
	if !decodeJSON(w, r, &input) {
		return
	}
	profile := input.model()
	applyRouteDefaults(&profile)
	if err := s.validateRouteModel(&profile); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_ROUTE_PROFILE", err.Error())
		return
	}
	if err := s.store.CreateRouteProfile(r.Context(), &profile); err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("route-profile.created", profile)
	writeJSON(w, http.StatusCreated, profile)
}
func (s *Server) updateRouteProfile(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetRouteProfile(r.Context(), id)
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	var input routeProfileInput
	if !decodeJSON(w, r, &input) {
		return
	}
	profile := input.model()
	profile.ID = id
	profile.CreatedAt = existing.CreatedAt
	profile.LastValidationAt = existing.LastValidationAt
	profile.LastValidationSucceeded = existing.LastValidationSucceeded
	profile.LastValidationSnapshot = existing.LastValidationSnapshot
	applyRouteDefaults(&profile)
	if err := s.validateRouteModel(&profile); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_ROUTE_PROFILE", err.Error())
		return
	}
	if err := s.store.UpdateRouteProfile(r.Context(), &profile); err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("route-profile.updated", profile)
	writeJSON(w, http.StatusOK, profile)
}
func (s *Server) deleteRouteProfile(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteRouteProfile(r.Context(), id); err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("route-profile.deleted", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) validateRouteProfile(w http.ResponseWriter, r *http.Request) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	profile, err := s.store.GetRouteProfile(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	validationContext, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	validation := s.routes.Validate(validationContext, profile)
	if err := s.store.UpdateRouteValidation(r.Context(), profile.ID, validation); err != nil {
		handleStoreError(w, r, err)
		return
	}
	s.broker.Publish("route.validation.completed", map[string]any{"routeProfileId": profile.ID, "validation": validation})
	status := http.StatusOK
	if !validation.Success {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, validation)
}

func applyRouteDefaults(profile *models.RouteProfile) {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Description = strings.TrimSpace(profile.Description)
	profile.InterfaceName = strings.TrimSpace(profile.InterfaceName)
	profile.SourceIP = strings.TrimSpace(profile.SourceIP)
	profile.ExpectedGateway = strings.TrimSpace(profile.ExpectedGateway)
	profile.ExpectedRoutingTable = strings.TrimSpace(profile.ExpectedRoutingTable)
	profile.ValidationTarget = strings.TrimSpace(profile.ValidationTarget)
	profile.Notes = strings.TrimSpace(profile.Notes)
	if profile.ValidationTarget == "" {
		profile.ValidationTarget = validationTarget(profile.SourceIP)
	}
	if profile.LastValidationSnapshot == nil {
		profile.LastValidationSnapshot = map[string]any{}
	}
}
func (s *Server) validateRouteModel(profile *models.RouteProfile) error {
	if err := validateRouteFields(profile); err != nil {
		return err
	}
	return s.interfaces.ValidateSource(profile.InterfaceName, profile.SourceIP)
}

func validateRouteFields(profile *models.RouteProfile) error {
	if len(profile.Name) < 1 || len(profile.Name) > 120 {
		return fmt.Errorf("name must contain between 1 and 120 characters")
	}
	if len(profile.Description) > 4000 || len(profile.Notes) > 8000 {
		return fmt.Errorf("description or notes exceed their size limit")
	}
	if err := validateInterfaceName(profile.InterfaceName); err != nil {
		return err
	}
	if net.ParseIP(profile.SourceIP) == nil {
		return fmt.Errorf("source IP is invalid")
	}
	if profile.ExpectedGateway != "" && net.ParseIP(profile.ExpectedGateway) == nil {
		return fmt.Errorf("expected gateway must be an IP address")
	}
	if err := validateRoutingTableToken(profile.ExpectedRoutingTable); err != nil {
		return err
	}
	if net.ParseIP(profile.ValidationTarget) == nil && (len(profile.ValidationTarget) > 253 || strings.ContainsAny(profile.ValidationTarget, " /\\\x00\r\n")) {
		return fmt.Errorf("validation target must be a hostname or IP address")
	}
	return nil
}

func validateInterfaceName(value string) error {
	if len(value) < 1 || len(value) > 64 || strings.ContainsRune(value, '/') || strings.ContainsRune(value, '\x00') || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return fmt.Errorf("interface name must contain between 1 and 64 non-whitespace characters without slashes")
	}
	return nil
}

func validateRoutingTableToken(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 64 {
		return fmt.Errorf("expected routing table must not exceed 64 characters")
	}
	if routingTableNumberPattern.MatchString(value) {
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return fmt.Errorf("numeric routing table ID must be between 0 and 4294967295")
		}
		return nil
	}
	if routingTableNamePattern.MatchString(value) {
		return nil
	}
	return fmt.Errorf("expected routing table must be a numeric ID or a safe Linux table name")
}

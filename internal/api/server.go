// Package api implements MultiSpeed's versioned same-origin HTTP API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/frontend"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	ooklaprovider "github.com/dude2k/MultiSpeed/internal/providers/ookla"
	"github.com/dude2k/MultiSpeed/internal/retention"
	"github.com/dude2k/MultiSpeed/internal/scheduler"
	"github.com/dude2k/MultiSpeed/internal/statistics"
	"github.com/go-chi/chi/v5"
)

type BuildInfo struct{ Version, GitCommit, BuildTime string }

type routeValidator interface {
	Validate(context.Context, models.RouteProfile) models.RouteValidation
}

type Server struct {
	store                        *database.Store
	scheduler                    *scheduler.Scheduler
	execution                    *execution.Manager
	interfaces                   *network.InterfaceService
	routes                       routeValidator
	providers                    *providers.Registry
	broker                       *events.Broker
	logger                       *slog.Logger
	build                        BuildInfo
	hosts                        hostPolicy
	statsService                 *statistics.Service
	retentionCleaner             *retention.Cleaner
	startedAt                    time.Time
	ready                        func(context.Context) error
	manualLimit                  *rateGate
	discoveryLimit               *rateGate
	ooklaUploadLimit             *rateGate
	ooklaBinary                  *ooklaprovider.BinaryManager
	ooklaEULAEnvironmentAccepted bool
	configurationMu              sync.Mutex
}

func New(store *database.Store, schedule *scheduler.Scheduler, executionManager *execution.Manager, interfaces *network.InterfaceService,
	routes routeValidator, registry *providers.Registry, broker *events.Broker, logger *slog.Logger, build BuildInfo, httpPolicy HTTPPolicy) *Server {
	cleaner, err := retention.New(store, logger, retention.Config{BatchSize: 500, MaxBatches: 100, RunMaintenance: true})
	if err != nil {
		panic(err)
	}
	return &Server{store: store, scheduler: schedule, execution: executionManager, interfaces: interfaces, routes: routes, providers: registry, broker: broker, logger: logger,
		build: build, hosts: newHostPolicy(httpPolicy, interfaces), statsService: statistics.New(store), retentionCleaner: cleaner, startedAt: time.Now().UTC(), ready: func(ctx context.Context) error {
			_, err := store.SchemaVersion(ctx)
			return err
		}, manualLimit: newRateGate(4, time.Minute), discoveryLimit: newRateGate(12, time.Minute), ooklaUploadLimit: newRateGate(2, time.Hour),
		ooklaBinary:                  ooklaprovider.NewBinaryManager(httpPolicy.DataDirectory, httpPolicy.OoklaBinaryPath, httpPolicy.AllowOoklaBinaryUpload, nil),
		ooklaEULAEnvironmentAccepted: httpPolicy.OoklaEULAEnvironmentAccepted}
}

func (s *Server) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(s.requestID, s.recoverer, s.securityHeaders, s.requestLog, s.trustedHost, s.sameOrigin, s.contentType)
	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/healthz", s.health)
		api.Get("/readyz", s.readyz)
		api.Get("/tasks", s.listTasks)
		api.Post("/tasks", s.createTask)
		api.Post("/tasks/validate", s.validateTransientTask)
		api.Get("/tasks/{id}", s.getTask)
		api.Put("/tasks/{id}", s.updateTask)
		api.Delete("/tasks/{id}", s.deleteTask)
		api.Post("/tasks/{id}/run", s.runTask)
		api.Post("/tasks/{id}/validate", s.validateTask)
		api.Post("/tasks/{id}/duplicate", s.duplicateTask)
		api.Get("/tasks/{id}/next-runs", s.nextRuns)
		api.Get("/results", s.listResults)
		api.Get("/results/dashboard-summary", s.dashboardResults)
		api.Get("/results/{id}", s.getResult)
		api.Delete("/results/{id}", s.deleteResult)
		api.Post("/results/delete-batch", s.deleteResultsBatch)
		api.Get("/statistics", s.statistics)
		api.Get("/interfaces", s.listInterfaces)
		api.Post("/interfaces/refresh", s.refreshInterfaces)
		api.Get("/route-profiles", s.listRouteProfiles)
		api.Post("/route-profiles", s.createRouteProfile)
		api.Get("/route-profiles/{id}", s.getRouteProfile)
		api.Put("/route-profiles/{id}", s.updateRouteProfile)
		api.Delete("/route-profiles/{id}", s.deleteRouteProfile)
		api.Post("/route-profiles/{id}/validate", s.validateRouteProfile)
		api.Get("/providers", s.listProviders)
		api.Get("/providers/ookla/binary", s.getOoklaBinaryStatus)
		api.Post("/providers/ookla/binary", s.uploadOoklaBinary)
		api.Get("/providers/{provider}/servers", s.listProviderServers)
		api.Post("/providers/{provider}/validate-server", s.validateProviderServer)
		api.Get("/settings", s.getSettings)
		api.Put("/settings", s.updateSettings)
		api.Put("/settings/ookla-eula", s.updateOoklaEULA)
		api.Post("/retention/cleanup", s.cleanupResults)
		api.Get("/exports/results.csv", s.exportResultsCSV)
		api.Get("/exports/results.json", s.exportResultsJSON)
		api.Get("/config/export", s.exportConfiguration)
		api.Post("/config/import", s.importConfiguration)
		api.Post("/backup", s.backup)
		api.Get("/events", s.events)
		api.Get("/system", s.system)
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested API resource was not found.")
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The requested method is not supported for this API resource.")
		})
	})
	router.Handle("/*", staticHandler())
	return router
}

func staticHandler() http.Handler {
	root, err := fs.Sub(frontend.Files, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			http.NotFound(writer, request)
			return
		}
		requested := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requested == "." || requested == "" {
			requested = "index.html"
		}
		if _, err := fs.Stat(root, requested); err != nil {
			requested = "index.html"
		}
		// net/http's FileServer intentionally redirects /index.html to ./.
		// Serve the SPA entry point directly so both / and client-side routes
		// return the document instead of entering a self-redirect loop.
		if requested == "index.html" {
			contents, err := fs.ReadFile(root, requested)
			if err != nil {
				http.Error(writer, "embedded frontend is unavailable", http.StatusInternalServerError)
				return
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(writer, request, requested, time.Time{}, bytes.NewReader(contents))
			return
		}
		if extension := path.Ext(requested); extension != "" {
			if contentType := mime.TypeByExtension(extension); contentType != "" {
				writer.Header().Set("Content-Type", contentType)
			}
		}
		clone := request.Clone(request.Context())
		clone.URL.Path = "/" + requested
		fileServer.ServeHTTP(writer, clone)
	})
}

type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	} `json:"error"`
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string) {
	var envelope errorEnvelope
	envelope.Error.Code = code
	envelope.Error.Message = message
	envelope.Error.RequestID = requestIDFrom(request.Context())
	writeJSON(writer, status, envelope)
}
func handleStoreError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, database.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.")
		return
	}
	if errors.Is(err, database.ErrActive) {
		writeError(writer, request, http.StatusConflict, "RESOURCE_ACTIVE", "The resource has an active execution and cannot be deleted.")
		return
	}
	if errors.Is(err, database.ErrInUse) {
		writeError(writer, request, http.StatusConflict, "RESOURCE_IN_USE", "The resource is still referenced by a task and cannot be deleted.")
		return
	}
	requestID := requestIDFrom(request.Context())
	slog.ErrorContext(request.Context(), "API persistence error", "request_id", requestID, "error", err)
	writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.")
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_JSON", fmt.Sprintf("The JSON request is invalid: %v", err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_JSON", "Only one JSON value is allowed.")
		return false
	}
	return true
}

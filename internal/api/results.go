package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/go-chi/chi/v5"
)

// resultSummary is the bounded representation returned by the collection
// endpoint. Large diagnostics remain available from GET /results/{id}.
type resultSummary struct {
	ID                      string              `json:"id"`
	TaskID                  string              `json:"taskId"`
	RouteProfileID          *string             `json:"routeProfileId"`
	Trigger                 models.TriggerType  `json:"trigger"`
	Provider                models.ProviderID   `json:"provider"`
	QueuedAt                time.Time           `json:"queuedAt"`
	ScheduledAt             *time.Time          `json:"scheduledAt"`
	StartedAt               *time.Time          `json:"startedAt"`
	FinishedAt              *time.Time          `json:"finishedAt"`
	Status                  models.ResultStatus `json:"status"`
	DownloadBitsPerSecond   *int64              `json:"downloadBitsPerSecond"`
	UploadBitsPerSecond     *int64              `json:"uploadBitsPerSecond"`
	LatencyMilliseconds     *float64            `json:"latencyMilliseconds"`
	JitterMilliseconds      *float64            `json:"jitterMilliseconds"`
	PacketLossPercent       *float64            `json:"packetLossPercent"`
	DownloadBytes           *int64              `json:"downloadBytes"`
	UploadBytes             *int64              `json:"uploadBytes"`
	SelectedInterface       string              `json:"selectedInterface"`
	SelectedSourceIP        string              `json:"selectedSourceIp"`
	DetectedPublicIP        string              `json:"detectedPublicIp"`
	ServerID                string              `json:"serverId"`
	ServerName              string              `json:"serverName"`
	ServerHost              string              `json:"serverHost"`
	ServerSponsor           string              `json:"serverSponsor"`
	ServerLocation          string              `json:"serverLocation"`
	ServerCountry           string              `json:"serverCountry"`
	ProviderResultURL       string              `json:"providerResultUrl"`
	CloudflareColo          string              `json:"cloudflareColo"`
	ExecutionDurationMS     int64               `json:"executionDurationMs"`
	ProcessExitCode         *int                `json:"processExitCode"`
	SanitizedError          string              `json:"sanitizedError"`
	ProviderVersion         string              `json:"providerVersion"`
	ApplicationVersion      string              `json:"applicationVersion"`
	TLSVerificationDisabled bool                `json:"tlsVerificationDisabled"`
}

type dashboardTaskSummary struct {
	TaskID        string         `json:"taskId"`
	TaskName      string         `json:"taskName"`
	Enabled       bool           `json:"enabled"`
	InterfaceName string         `json:"interfaceName"`
	SourceIP      string         `json:"sourceIp"`
	LatestResult  *resultSummary `json:"latestResult"`
}

type dashboardPathSummary struct {
	InterfaceName string         `json:"interfaceName"`
	SourceIP      string         `json:"sourceIp"`
	TaskIDs       []string       `json:"taskIds"`
	LatestResult  *resultSummary `json:"latestResult"`
}

type dashboardResultSummary struct {
	LatestByTask    []dashboardTaskSummary `json:"latestByTask"`
	LatestByPath    []dashboardPathSummary `json:"latestByPath"`
	ActiveRuns      []resultSummary        `json:"activeRuns"`
	RecentFailures  []resultSummary        `json:"recentFailures"`
	FailedTaskCount int                    `json:"failedTaskCount"`
}

func compactResult(result models.Result) resultSummary {
	return resultSummary{
		ID: result.ID, TaskID: result.TaskID, RouteProfileID: result.RouteProfileID, Trigger: result.Trigger,
		Provider: result.Provider, QueuedAt: result.QueuedAt, ScheduledAt: result.ScheduledAt, StartedAt: result.StartedAt,
		FinishedAt: result.FinishedAt, Status: result.Status, DownloadBitsPerSecond: result.DownloadBitsPerSecond,
		UploadBitsPerSecond: result.UploadBitsPerSecond, LatencyMilliseconds: result.LatencyMilliseconds,
		JitterMilliseconds: result.JitterMilliseconds, PacketLossPercent: result.PacketLossPercent,
		DownloadBytes: result.DownloadBytes, UploadBytes: result.UploadBytes, SelectedInterface: result.SelectedInterface,
		SelectedSourceIP: result.SelectedSourceIP, DetectedPublicIP: result.DetectedPublicIP, ServerID: result.ServerID,
		ServerName: result.ServerName, ServerHost: result.ServerHost, ServerSponsor: result.ServerSponsor,
		ServerLocation: result.ServerLocation, ServerCountry: result.ServerCountry, ProviderResultURL: result.ProviderResultURL,
		CloudflareColo: result.CloudflareColo, ExecutionDurationMS: result.ExecutionDurationMS,
		ProcessExitCode: result.ProcessExitCode, SanitizedError: result.SanitizedError, ProviderVersion: result.ProviderVersion,
		ApplicationVersion: result.ApplicationVersion, TLSVerificationDisabled: result.TLSVerificationDisabled,
	}
}

func compactResults(results []models.Result) []resultSummary {
	items := make([]resultSummary, len(results))
	for index := range results {
		items[index] = compactResult(results[index])
	}
	return items
}

func (s *Server) listResults(w http.ResponseWriter, r *http.Request) {
	filter, err := parseResultFilter(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	page, err := s.store.ListResults(r.Context(), filter)
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	items := make([]resultSummary, len(page.Items))
	for index := range page.Items {
		items[index] = compactResult(page.Items[index])
	}
	writeJSON(w, http.StatusOK, models.Page[resultSummary]{
		Items: items, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages,
	})
}

func (s *Server) dashboardResults(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.store.GetDashboardResults(r.Context())
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	latestByTask := make(map[string]resultSummary, len(snapshot.LatestByTask))
	for _, result := range snapshot.LatestByTask {
		latestByTask[result.TaskID] = compactResult(result)
	}
	type pathKey struct{ interfaceName, sourceIP string }
	latestByPath := make(map[pathKey]resultSummary, len(snapshot.LatestByPath))
	for _, result := range snapshot.LatestByPath {
		latestByPath[pathKey{result.SelectedInterface, result.SelectedSourceIP}] = compactResult(result)
	}

	response := dashboardResultSummary{
		LatestByTask: make([]dashboardTaskSummary, 0, len(snapshot.Tasks)),
		LatestByPath: make([]dashboardPathSummary, 0, len(snapshot.Tasks)),
		ActiveRuns:   compactResults(snapshot.ActiveRuns), RecentFailures: compactResults(snapshot.RecentFailures),
	}
	pathIndexes := make(map[pathKey]int, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		var latest *resultSummary
		if value, ok := latestByTask[task.ID]; ok {
			valueCopy := value
			latest = &valueCopy
			if value.Status == models.StatusFailed {
				response.FailedTaskCount++
			}
		}
		response.LatestByTask = append(response.LatestByTask, dashboardTaskSummary{
			TaskID: task.ID, TaskName: task.Name, Enabled: task.Enabled, InterfaceName: task.InterfaceName,
			SourceIP: task.SourceIP, LatestResult: latest,
		})
		key := pathKey{task.InterfaceName, task.SourceIP}
		index, exists := pathIndexes[key]
		if !exists {
			pathLatest := (*resultSummary)(nil)
			if value, ok := latestByPath[key]; ok {
				valueCopy := value
				pathLatest = &valueCopy
			}
			response.LatestByPath = append(response.LatestByPath, dashboardPathSummary{
				InterfaceName: task.InterfaceName, SourceIP: task.SourceIP, TaskIDs: make([]string, 0, 1), LatestResult: pathLatest,
			})
			index = len(response.LatestByPath) - 1
			pathIndexes[key] = index
		}
		response.LatestByPath[index].TaskIDs = append(response.LatestByPath[index].TaskIDs, task.ID)
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *Server) getResult(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetResult(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		handleStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) deleteResult(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteResult(r.Context(), chi.URLParam(r, "id")); err != nil {
		handleStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) deleteResultsBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	count, err := s.store.DeleteResults(r.Context(), body.IDs)
	if err != nil {
		if errors.Is(err, database.ErrActive) {
			handleStoreError(w, r, err)
			return
		}
		writeError(w, r, http.StatusBadRequest, "INVALID_BATCH", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": count})
}

func parseResultFilter(r *http.Request) (database.ResultFilter, error) {
	query := r.URL.Query()
	provider := strings.TrimSpace(query.Get("provider"))
	if provider != "" && provider != string(models.ProviderOokla) && provider != string(models.ProviderLibreSpeed) && provider != string(models.ProviderCloudflare) {
		return database.ResultFilter{}, errors.New("provider must be ookla, librespeed, or cloudflare")
	}
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && status != string(models.StatusQueued) && status != string(models.StatusValidating) &&
		status != string(models.StatusRunning) && status != string(models.StatusSucceeded) && status != string(models.StatusFailed) &&
		status != string(models.StatusSkipped) && status != string(models.StatusCancelled) {
		return database.ResultFilter{}, errors.New("status is not supported")
	}
	sort := strings.TrimSpace(query.Get("sort"))
	if sort == "" {
		sort = "startedAt"
	}
	if sort != "startedAt" && sort != "finishedAt" && sort != "download" && sort != "upload" && sort != "latency" {
		return database.ResultFilter{}, errors.New("sort is not supported")
	}
	direction := strings.TrimSpace(query.Get("direction"))
	if direction == "" {
		direction = "desc"
	}
	if direction != "asc" && direction != "desc" {
		return database.ResultFilter{}, errors.New("direction must be asc or desc")
	}
	filter := database.ResultFilter{
		TaskID: strings.TrimSpace(query.Get("taskId")), Provider: provider, Status: status,
		Interface: strings.TrimSpace(query.Get("interface")), SourceIP: strings.TrimSpace(query.Get("sourceIp")),
		ServerID: strings.TrimSpace(query.Get("serverId")), Sort: sort, Descending: direction == "desc",
	}
	if filter.Interface == "" {
		filter.Interface = strings.TrimSpace(query.Get("interfaceName"))
	}
	var err error
	if query.Get("page") != "" {
		filter.Page, err = strconv.Atoi(query.Get("page"))
		if err != nil {
			return filter, errors.New("page must be an integer")
		}
		if filter.Page < 1 {
			return filter, errors.New("page must be at least 1")
		}
	}
	if query.Get("pageSize") != "" {
		filter.PageSize, err = strconv.Atoi(query.Get("pageSize"))
		if err != nil {
			return filter, errors.New("pageSize must be an integer")
		}
		if filter.PageSize < 1 || filter.PageSize > database.MaxResultPageSize {
			return filter, fmt.Errorf("pageSize must be between 1 and %d", database.MaxResultPageSize)
		}
	}
	if query.Get("from") != "" {
		value, parseErr := time.Parse(time.RFC3339, query.Get("from"))
		if parseErr != nil {
			return filter, errors.New("from must be RFC3339")
		}
		filter.From = &value
	}
	if query.Get("to") != "" {
		value, parseErr := time.Parse(time.RFC3339, query.Get("to"))
		if parseErr != nil {
			return filter, errors.New("to must be RFC3339")
		}
		filter.To = &value
	}
	if filter.From != nil && filter.To != nil && !filter.To.After(*filter.From) {
		return filter, errors.New("to must be later than from")
	}
	return filter, nil
}

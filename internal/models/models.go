// Package models defines the provider-neutral persisted and API data model.
package models

import "time"

// CurrentOoklaEULAVersion identifies the terms revision reviewed for this
// MultiSpeed release. Bumping it invalidates older persisted acknowledgements
// without deleting their audit metadata.
const CurrentOoklaEULAVersion = "speedtest-eula-reviewed-2026-08-07"

const (
	LibreSpeedBitsMethodologyVersion = "multispeed-bps-v2"
	CloudflareLegacyUploadVersion    = "cloudflare-native-bounded-v1"
)

type ProviderID string

const (
	ProviderOokla      ProviderID = "ookla"
	ProviderLibreSpeed ProviderID = "librespeed"
	ProviderCloudflare ProviderID = "cloudflare"
)

type ResultStatus string

const (
	StatusQueued     ResultStatus = "queued"
	StatusValidating ResultStatus = "validating"
	StatusRunning    ResultStatus = "running"
	StatusSucceeded  ResultStatus = "succeeded"
	StatusFailed     ResultStatus = "failed"
	StatusSkipped    ResultStatus = "skipped"
	StatusCancelled  ResultStatus = "cancelled"
)

type TriggerType string

const (
	TriggerManual    TriggerType = "manual"
	TriggerScheduled TriggerType = "scheduled"
)

type Task struct {
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	Enabled                bool           `json:"enabled"`
	Provider               ProviderID     `json:"provider"`
	CronExpression         string         `json:"cronExpression"`
	Timezone               string         `json:"timezone"`
	RandomJitterSeconds    int            `json:"randomJitterSeconds"`
	ServerSelectionMode    string         `json:"serverSelectionMode"`
	ServerID               string         `json:"serverId"`
	ServerURL              string         `json:"serverUrl"`
	CustomServerDefinition map[string]any `json:"customServerDefinition"`
	InterfaceName          string         `json:"interfaceName"`
	SourceIP               string         `json:"sourceIp"`
	IPFamily               string         `json:"ipFamily"`
	RouteProfileID         *string        `json:"routeProfileId"`
	TimeoutSeconds         int            `json:"timeoutSeconds"`
	ProviderOptions        map[string]any `json:"providerOptions"`
	PreventOverlap         bool           `json:"preventOverlap"`
	RouteValidation        string         `json:"routeValidation"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
	LastScheduledAt        *time.Time     `json:"lastScheduledAt"`
	NextScheduledAt        *time.Time     `json:"nextScheduledAt"`
	DeletedAt              *time.Time     `json:"-"`
	NetworkPathValid       bool           `json:"networkPathValid"`
	NetworkPathMessage     string         `json:"networkPathMessage"`
}

type Result struct {
	ID                      string         `json:"id"`
	TaskID                  string         `json:"taskId"`
	RouteProfileID          *string        `json:"routeProfileId"`
	Trigger                 TriggerType    `json:"trigger"`
	Provider                ProviderID     `json:"provider"`
	QueuedAt                time.Time      `json:"queuedAt"`
	ScheduledAt             *time.Time     `json:"scheduledAt"`
	StartedAt               *time.Time     `json:"startedAt"`
	FinishedAt              *time.Time     `json:"finishedAt"`
	Status                  ResultStatus   `json:"status"`
	DownloadBitsPerSecond   *int64         `json:"downloadBitsPerSecond"`
	UploadBitsPerSecond     *int64         `json:"uploadBitsPerSecond"`
	LatencyMilliseconds     *float64       `json:"latencyMilliseconds"`
	JitterMilliseconds      *float64       `json:"jitterMilliseconds"`
	PacketLossPercent       *float64       `json:"packetLossPercent"`
	DownloadBytes           *int64         `json:"downloadBytes"`
	UploadBytes             *int64         `json:"uploadBytes"`
	SelectedInterface       string         `json:"selectedInterface"`
	SelectedSourceIP        string         `json:"selectedSourceIp"`
	DetectedPublicIP        string         `json:"detectedPublicIp"`
	ServerID                string         `json:"serverId"`
	ServerName              string         `json:"serverName"`
	ServerHost              string         `json:"serverHost"`
	ServerSponsor           string         `json:"serverSponsor"`
	ServerLocation          string         `json:"serverLocation"`
	ServerCountry           string         `json:"serverCountry"`
	ProviderResultURL       string         `json:"providerResultUrl"`
	CloudflareColo          string         `json:"cloudflareColo"`
	RouteValidationSnapshot map[string]any `json:"routeValidationSnapshot"`
	ExecutionDurationMS     int64          `json:"executionDurationMs"`
	ProcessExitCode         *int           `json:"processExitCode"`
	SanitizedError          string         `json:"sanitizedError"`
	RawProviderResponse     string         `json:"rawProviderResponse"`
	ProviderVersion         string         `json:"providerVersion"`
	ApplicationVersion      string         `json:"applicationVersion"`
	TLSVerificationDisabled bool           `json:"tlsVerificationDisabled"`
}

type RouteProfile struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Description             string         `json:"description"`
	InterfaceName           string         `json:"interfaceName"`
	SourceIP                string         `json:"sourceIp"`
	ExpectedGateway         string         `json:"expectedGateway"`
	ExpectedRoutingTable    string         `json:"expectedRoutingTable"`
	ValidationTarget        string         `json:"validationTarget"`
	Notes                   string         `json:"notes"`
	CreatedAt               time.Time      `json:"createdAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
	LastValidationAt        *time.Time     `json:"lastValidationAt"`
	LastValidationSucceeded *bool          `json:"lastValidationSucceeded"`
	LastValidationSnapshot  map[string]any `json:"lastValidationSnapshot"`
	DeletedAt               *time.Time     `json:"-"`
}

type Settings struct {
	DisplayUnits                string     `json:"displayUnits"`
	DefaultTimezone             string     `json:"defaultTimezone"`
	GlobalConcurrency           int        `json:"globalConcurrency"`
	AllowSeparateWANConcurrency bool       `json:"allowSeparateWanConcurrency"`
	RetentionMode               string     `json:"retentionMode"`
	RetentionValue              int        `json:"retentionValue"`
	DefaultChartRange           string     `json:"defaultChartRange"`
	InterfaceRefreshInterval    int        `json:"interfaceRefreshIntervalSeconds"`
	DefaultTaskTimeout          int        `json:"defaultTaskTimeoutSeconds"`
	DatabaseMaintenanceSchedule string     `json:"databaseMaintenanceSchedule"`
	OoklaEULAAccepted           bool       `json:"ooklaEulaAccepted"`
	OoklaEULAAcceptedAt         *time.Time `json:"ooklaEulaAcceptedAt"`
	OoklaEULAVersion            string     `json:"ooklaEulaVersion"`
	OoklaEULACurrentVersion     string     `json:"ooklaEulaCurrentVersion"`
	OoklaEULAEffectiveAccepted  bool       `json:"ooklaEulaEffectiveAccepted"`
	OoklaEULAAcceptanceSource   string     `json:"ooklaEulaAcceptanceSource"`
}

type InterfaceAddress struct {
	Address   string `json:"address"`
	Family    string `json:"family"`
	LinkLocal bool   `json:"linkLocal"`
}

type NetworkInterface struct {
	Name             string             `json:"name"`
	Index            int                `json:"index"`
	Operational      bool               `json:"operational"`
	OperationalState string             `json:"operationalState"`
	Loopback         bool               `json:"loopback"`
	Virtual          bool               `json:"virtual"`
	MACAddress       string             `json:"macAddress"`
	MTU              int                `json:"mtu"`
	Addresses        []InterfaceAddress `json:"addresses"`
}

type RouteValidation struct {
	Success          bool      `json:"success"`
	InterfaceName    string    `json:"interfaceName"`
	SourceIP         string    `json:"sourceIp"`
	Gateway          string    `json:"gateway"`
	RoutingTable     string    `json:"routingTable"`
	Destination      string    `json:"destination"`
	DetectedPublicIP string    `json:"detectedPublicIp"`
	Reachable        bool      `json:"reachable"`
	DurationMS       int64     `json:"durationMs"`
	ValidatedAt      time.Time `json:"validatedAt"`
	Message          string    `json:"message"`
}

type Page[T any] struct {
	Items      []T `json:"items"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

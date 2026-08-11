package models

import "time"

const (
	ConfigurationFormat        = "multispeed-config"
	ConfigurationFormatVersion = 1
)

// ConfigurationDocument is the portable, versioned subset of MultiSpeed's
// persisted state. Measurement history and provider terms acknowledgements are
// deliberately excluded.
type ConfigurationDocument struct {
	Format             string                      `json:"format"`
	Version            int                         `json:"version"`
	ExportedAt         time.Time                   `json:"exportedAt"`
	ApplicationVersion string                      `json:"applicationVersion"`
	Settings           ConfigurationSettings       `json:"settings"`
	RouteProfiles      []ConfigurationRouteProfile `json:"routeProfiles"`
	Tasks              []ConfigurationTask         `json:"tasks"`
}

type ConfigurationSettings struct {
	DisplayUnits                string `json:"displayUnits"`
	DefaultTimezone             string `json:"defaultTimezone"`
	GlobalConcurrency           int    `json:"globalConcurrency"`
	AllowSeparateWANConcurrency bool   `json:"allowSeparateWanConcurrency"`
	RetentionMode               string `json:"retentionMode"`
	RetentionValue              int    `json:"retentionValue"`
	DefaultChartRange           string `json:"defaultChartRange"`
	InterfaceRefreshInterval    int    `json:"interfaceRefreshIntervalSeconds"`
	DefaultTaskTimeout          int    `json:"defaultTaskTimeoutSeconds"`
	DatabaseMaintenanceSchedule string `json:"databaseMaintenanceSchedule"`
}

type ConfigurationRouteProfile struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	InterfaceName        string `json:"interfaceName"`
	SourceIP             string `json:"sourceIp"`
	ExpectedGateway      string `json:"expectedGateway"`
	ExpectedRoutingTable string `json:"expectedRoutingTable"`
	ValidationTarget     string `json:"validationTarget"`
	Notes                string `json:"notes"`
}

type ConfigurationTask struct {
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
}

type ConfigurationImportResult struct {
	ImportedAt        time.Time `json:"importedAt"`
	TaskCount         int       `json:"taskCount"`
	RouteProfileCount int       `json:"routeProfileCount"`
	SettingsUpdated   bool      `json:"settingsUpdated"`
}

func ConfigurationSettingsFrom(settings Settings) ConfigurationSettings {
	return ConfigurationSettings{
		DisplayUnits: settings.DisplayUnits, DefaultTimezone: settings.DefaultTimezone,
		GlobalConcurrency: settings.GlobalConcurrency, AllowSeparateWANConcurrency: settings.AllowSeparateWANConcurrency,
		RetentionMode: settings.RetentionMode, RetentionValue: settings.RetentionValue, DefaultChartRange: settings.DefaultChartRange,
		InterfaceRefreshInterval: settings.InterfaceRefreshInterval, DefaultTaskTimeout: settings.DefaultTaskTimeout,
		DatabaseMaintenanceSchedule: settings.DatabaseMaintenanceSchedule,
	}
}

func (settings ConfigurationSettings) Model() Settings {
	return Settings{
		DisplayUnits: settings.DisplayUnits, DefaultTimezone: settings.DefaultTimezone,
		GlobalConcurrency: settings.GlobalConcurrency, AllowSeparateWANConcurrency: settings.AllowSeparateWANConcurrency,
		RetentionMode: settings.RetentionMode, RetentionValue: settings.RetentionValue, DefaultChartRange: settings.DefaultChartRange,
		InterfaceRefreshInterval: settings.InterfaceRefreshInterval, DefaultTaskTimeout: settings.DefaultTaskTimeout,
		DatabaseMaintenanceSchedule: settings.DatabaseMaintenanceSchedule,
	}
}

func ConfigurationRouteProfileFrom(profile RouteProfile) ConfigurationRouteProfile {
	return ConfigurationRouteProfile{
		ID: profile.ID, Name: profile.Name, Description: profile.Description, InterfaceName: profile.InterfaceName,
		SourceIP: profile.SourceIP, ExpectedGateway: profile.ExpectedGateway, ExpectedRoutingTable: profile.ExpectedRoutingTable,
		ValidationTarget: profile.ValidationTarget, Notes: profile.Notes,
	}
}

func (profile ConfigurationRouteProfile) Model() RouteProfile {
	return RouteProfile{
		ID: profile.ID, Name: profile.Name, Description: profile.Description, InterfaceName: profile.InterfaceName,
		SourceIP: profile.SourceIP, ExpectedGateway: profile.ExpectedGateway, ExpectedRoutingTable: profile.ExpectedRoutingTable,
		ValidationTarget: profile.ValidationTarget, Notes: profile.Notes, LastValidationSnapshot: map[string]any{},
	}
}

func ConfigurationTaskFrom(task Task) ConfigurationTask {
	return ConfigurationTask{
		ID: task.ID, Name: task.Name, Description: task.Description, Enabled: task.Enabled, Provider: task.Provider,
		CronExpression: task.CronExpression, Timezone: task.Timezone, RandomJitterSeconds: task.RandomJitterSeconds,
		ServerSelectionMode: task.ServerSelectionMode, ServerID: task.ServerID, ServerURL: task.ServerURL,
		CustomServerDefinition: task.CustomServerDefinition, InterfaceName: task.InterfaceName, SourceIP: task.SourceIP,
		IPFamily: task.IPFamily, RouteProfileID: task.RouteProfileID, TimeoutSeconds: task.TimeoutSeconds,
		ProviderOptions: task.ProviderOptions, PreventOverlap: task.PreventOverlap, RouteValidation: task.RouteValidation,
	}
}

func (task ConfigurationTask) Model() Task {
	return Task{
		ID: task.ID, Name: task.Name, Description: task.Description, Enabled: task.Enabled, Provider: task.Provider,
		CronExpression: task.CronExpression, Timezone: task.Timezone, RandomJitterSeconds: task.RandomJitterSeconds,
		ServerSelectionMode: task.ServerSelectionMode, ServerID: task.ServerID, ServerURL: task.ServerURL,
		CustomServerDefinition: task.CustomServerDefinition, InterfaceName: task.InterfaceName, SourceIP: task.SourceIP,
		IPFamily: task.IPFamily, RouteProfileID: task.RouteProfileID, TimeoutSeconds: task.TimeoutSeconds,
		ProviderOptions: task.ProviderOptions, PreventOverlap: task.PreventOverlap, RouteValidation: task.RouteValidation,
	}
}

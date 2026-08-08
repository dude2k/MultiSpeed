// Package statistics calculates timezone-aware result aggregates without
// relying on SQLite's UTC-only date grouping functions.
package statistics

import "time"

// Granularity controls how result timestamps are bucketed.
type Granularity string

const (
	GranularityRaw     Granularity = "raw"
	GranularityDay     Granularity = "day"
	GranularityISOWeek Granularity = "iso-week"
	GranularityMonth   Granularity = "month"
	GranularityYear    Granularity = "year"
	// GranularityCustom creates one aggregate bucket for the requested range.
	GranularityCustom Granularity = "custom"
)

// Dimension controls how independent comparison series are formed.
type Dimension string

const (
	DimensionNone         Dimension = ""
	DimensionTask         Dimension = "task"
	DimensionInterface    Dimension = "interface"
	DimensionSourceIP     Dimension = "source-ip"
	DimensionProvider     Dimension = "provider"
	DimensionServer       Dimension = "server"
	DimensionRouteProfile Dimension = "route-profile"
	DimensionPublicIP     Dimension = "public-ip"
)

// Filter selects comparison members. Empty slices include every value.
type Filter struct {
	TaskIDs         []string `json:"taskIds,omitempty"`
	Interfaces      []string `json:"interfaces,omitempty"`
	SourceIPs       []string `json:"sourceIps,omitempty"`
	Providers       []string `json:"providers,omitempty"`
	ServerIDs       []string `json:"serverIds,omitempty"`
	RouteProfileIDs []string `json:"routeProfileIds,omitempty"`
	PublicIPs       []string `json:"publicIps,omitempty"`
}

// Query specifies a half-open UTC instant range [From, To). ReportingTimezone
// affects bucket boundaries and labels, not the persisted timestamps.
type Query struct {
	From              time.Time   `json:"from"`
	To                time.Time   `json:"to"`
	Granularity       Granularity `json:"granularity"`
	ReportingTimezone string      `json:"reportingTimezone"`
	GroupBy           Dimension   `json:"groupBy,omitempty"`
	Filter            Filter      `json:"filter,omitempty"`
}

// Report is a set of comparable series over a normalized query.
type Report struct {
	From              time.Time   `json:"from"`
	To                time.Time   `json:"to"`
	Granularity       Granularity `json:"granularity"`
	ReportingTimezone string      `json:"reportingTimezone"`
	GroupBy           Dimension   `json:"groupBy,omitempty"`
	TotalResults      int         `json:"totalResults"`
	Overall           Bucket      `json:"overall"`
	Groups            []Group     `json:"groups"`
}

// Group contains the buckets for one comparison value.
type Group struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Overall Bucket   `json:"overall"`
	Buckets []Bucket `json:"buckets"`
}

// Bucket is one raw result or one calendar/range aggregate.
type Bucket struct {
	Start              time.Time `json:"start"`
	End                time.Time `json:"end"`
	Label              string    `json:"label"`
	ResultID           string    `json:"resultId,omitempty"`
	Counts             Counts    `json:"counts"`
	SuccessRatePercent *float64  `json:"successRatePercent"`
	FailureRatePercent *float64  `json:"failureRatePercent"`
	Metrics            Metrics   `json:"metrics"`
}

// Counts separates result lifecycle outcomes from metric sample counts.
type Counts struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Cancelled  int `json:"cancelled"`
	Other      int `json:"other"`
}

// Metrics contains summaries in the normalized persisted units.
type Metrics struct {
	DownloadBitsPerSecond         Summary `json:"downloadBitsPerSecond"`
	UploadBitsPerSecond           Summary `json:"uploadBitsPerSecond"`
	LatencyMilliseconds           Summary `json:"latencyMilliseconds"`
	JitterMilliseconds            Summary `json:"jitterMilliseconds"`
	PacketLossPercent             Summary `json:"packetLossPercent"`
	ExecutionDurationMilliseconds Summary `json:"executionDurationMilliseconds"`
}

// Summary describes the finite, non-negative eligible samples for a metric.
// Numeric fields are null when Count is zero.
type Summary struct {
	Count             int      `json:"count"`
	Minimum           *float64 `json:"minimum"`
	Maximum           *float64 `json:"maximum"`
	Average           *float64 `json:"average"`
	Median            *float64 `json:"median"`
	P95               *float64 `json:"p95"`
	StandardDeviation *float64 `json:"standardDeviation"`
}

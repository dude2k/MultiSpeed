package statistics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
)

var (
	ErrResultLimit    = errors.New("statistics result limit exceeded")
	ErrOutputLimit    = errors.New("statistics output limit exceeded")
	ErrDimensionLimit = errors.New("statistics comparison dimension limit exceeded")
)

const maxStatisticsResults = 50_000

const (
	// MaxStatisticsOutputPoints bounds the fully materialized report. It counts
	// the report-wide overall bucket, every group-wide overall bucket, and every
	// time bucket returned inside a group.
	MaxStatisticsOutputPoints = 5_000
	// MaxStatisticsGroups separately bounds comparison-cardinality even when a
	// coarse granularity would otherwise keep the bucket count low.
	MaxStatisticsGroups = 1_000
	// MaxStatisticsDimensionBytes bounds persisted/provider-owned comparison
	// values before they become map keys or are copied into response groups.
	MaxStatisticsDimensionBytes = 512
)

// ResultSource is implemented by database.Store and kept as an interface for
// deterministic aggregation tests.
type ResultSource interface {
	WalkStatisticsResults(context.Context, database.StatisticsFilter, func(database.StatisticsResult) error) error
}

type Service struct {
	source ResultSource
}

func New(source ResultSource) *Service {
	return &Service{source: source}
}

// Query streams the selected rows and returns timezone-aware aggregates.
func (s *Service) Query(ctx context.Context, query Query) (Report, error) {
	normalized, location, err := normalizeQuery(query)
	if err != nil {
		return Report{}, err
	}
	if s == nil || s.source == nil {
		return Report{}, errors.New("statistics result source is not configured")
	}

	report := Report{
		From:              normalized.From,
		To:                normalized.To,
		Granularity:       normalized.Granularity,
		ReportingTimezone: normalized.ReportingTimezone,
		GroupBy:           normalized.GroupBy,
		Groups:            make([]Group, 0),
	}
	overall := &bucketAccumulator{descriptor: fullRangeBucket(normalized)}
	groups := make(map[string]*groupAccumulator)
	outputPoints := 1 // report.Overall
	databaseFilter := database.StatisticsFilter{
		From:            normalized.From,
		To:              normalized.To,
		TaskIDs:         normalized.Filter.TaskIDs,
		Interfaces:      normalized.Filter.Interfaces,
		SourceIPs:       normalized.Filter.SourceIPs,
		Providers:       normalized.Filter.Providers,
		ServerIDs:       normalized.Filter.ServerIDs,
		RouteProfileIDs: normalized.Filter.RouteProfileIDs,
		PublicIPs:       normalized.Filter.PublicIPs,
	}
	err = s.source.WalkStatisticsResults(ctx, databaseFilter, func(result database.StatisticsResult) error {
		if report.TotalResults >= maxStatisticsResults {
			return ErrResultLimit
		}
		if result.Timestamp.Before(normalized.From) || !result.Timestamp.Before(normalized.To) {
			return nil
		}
		key, label := comparisonGroup(result, normalized.GroupBy)
		if len(key) > MaxStatisticsDimensionBytes || len(label) > MaxStatisticsDimensionBytes {
			return fmt.Errorf("%w: comparison dimension exceeds %d bytes", ErrDimensionLimit, MaxStatisticsDimensionBytes)
		}
		group := groups[key]
		if group == nil {
			// A new group always creates two output points: Group.Overall and
			// its first Group.Bucket. Check both limits before allocating either
			// accumulator or adding anything to the report maps.
			if len(groups) >= MaxStatisticsGroups || outputPoints > MaxStatisticsOutputPoints-2 {
				return ErrOutputLimit
			}
			descriptor := describeBucket(result, normalized, location)
			bucket := &bucketAccumulator{descriptor: descriptor}
			group = &groupAccumulator{
				key: key, label: label, buckets: map[string]*bucketAccumulator{descriptor.key: bucket},
				overall: &bucketAccumulator{descriptor: fullRangeBucket(normalized)},
			}
			groups[key] = group
			outputPoints += 2
			bucket.add(result)
		} else {
			descriptor := describeBucket(result, normalized, location)
			bucket := group.buckets[descriptor.key]
			if bucket == nil {
				if outputPoints >= MaxStatisticsOutputPoints {
					return ErrOutputLimit
				}
				bucket = &bucketAccumulator{descriptor: descriptor}
				group.buckets[descriptor.key] = bucket
				outputPoints++
			}
			bucket.add(result)
		}
		group.overall.add(result)
		overall.add(result)
		report.TotalResults++
		return nil
	})
	if err != nil {
		return Report{}, fmt.Errorf("calculate statistics: %w", err)
	}
	report.Overall = overall.finish()

	orderedGroups := make([]*groupAccumulator, 0, len(groups))
	for _, group := range groups {
		orderedGroups = append(orderedGroups, group)
	}
	sort.Slice(orderedGroups, func(i, j int) bool {
		if orderedGroups[i].label == orderedGroups[j].label {
			return orderedGroups[i].key < orderedGroups[j].key
		}
		return orderedGroups[i].label < orderedGroups[j].label
	})
	for _, group := range orderedGroups {
		report.Groups = append(report.Groups, group.finish())
	}
	return report, nil
}

func fullRangeBucket(query Query) bucketDescriptor {
	return bucketDescriptor{
		key: "overall", label: "Full range", start: query.From, end: query.To,
	}
}

func normalizeQuery(query Query) (Query, *time.Location, error) {
	if query.From.IsZero() || query.To.IsZero() || !query.To.After(query.From) {
		return Query{}, nil, errors.New("statistics time range must have a non-zero end after start")
	}
	query.From = query.From.UTC()
	query.To = query.To.UTC()
	if query.Granularity == "" {
		query.Granularity = GranularityDay
	}
	if query.Granularity == "week" {
		query.Granularity = GranularityISOWeek
	}
	switch query.Granularity {
	case GranularityRaw, GranularityDay, GranularityISOWeek, GranularityMonth, GranularityYear, GranularityCustom:
	default:
		return Query{}, nil, fmt.Errorf("unsupported statistics granularity %q", query.Granularity)
	}
	if query.ReportingTimezone == "" {
		query.ReportingTimezone = "UTC"
	}
	location, err := time.LoadLocation(query.ReportingTimezone)
	if err != nil {
		return Query{}, nil, fmt.Errorf("load reporting timezone %q: %w", query.ReportingTimezone, err)
	}
	switch query.GroupBy {
	case DimensionNone, DimensionTask, DimensionInterface, DimensionSourceIP, DimensionProvider,
		DimensionServer, DimensionRouteProfile, DimensionPublicIP:
	default:
		return Query{}, nil, fmt.Errorf("unsupported statistics comparison dimension %q", query.GroupBy)
	}
	query.Filter.TaskIDs = normalizeValues(query.Filter.TaskIDs)
	query.Filter.Interfaces = normalizeValues(query.Filter.Interfaces)
	query.Filter.SourceIPs = normalizeValues(query.Filter.SourceIPs)
	query.Filter.Providers = normalizeValues(query.Filter.Providers)
	query.Filter.ServerIDs = normalizeValues(query.Filter.ServerIDs)
	query.Filter.RouteProfileIDs = normalizeValues(query.Filter.RouteProfileIDs)
	query.Filter.PublicIPs = normalizeValues(query.Filter.PublicIPs)
	return query, location, nil
}

func normalizeValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

type bucketDescriptor struct {
	key      string
	label    string
	resultID string
	start    time.Time
	end      time.Time
}

func describeBucket(result database.StatisticsResult, query Query, location *time.Location) bucketDescriptor {
	local := result.Timestamp.In(location)
	switch query.Granularity {
	case GranularityRaw:
		return bucketDescriptor{
			key:      result.Timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + result.ID,
			label:    local.Format(time.RFC3339Nano),
			resultID: result.ID,
			start:    result.Timestamp.UTC(),
			end:      result.Timestamp.UTC(),
		}
	case GranularityCustom:
		return bucketDescriptor{
			key:   "custom",
			label: query.From.In(location).Format(time.RFC3339) + " - " + query.To.In(location).Format(time.RFC3339),
			start: query.From,
			end:   query.To,
		}
	case GranularityDay:
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		return calendarBucket(start, start.AddDate(0, 0, 1), start.Format("2006-01-02"))
	case GranularityISOWeek:
		isoYear, isoWeek := local.ISOWeek()
		weekdayOffset := (int(local.Weekday()) + 6) % 7
		midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
		start := midnight.AddDate(0, 0, -weekdayOffset)
		return calendarBucket(start, start.AddDate(0, 0, 7), fmt.Sprintf("%04d-W%02d", isoYear, isoWeek))
	case GranularityMonth:
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		return calendarBucket(start, start.AddDate(0, 1, 0), start.Format("2006-01"))
	case GranularityYear:
		start := time.Date(local.Year(), time.January, 1, 0, 0, 0, 0, location)
		return calendarBucket(start, start.AddDate(1, 0, 0), start.Format("2006"))
	default:
		panic("validated granularity is not handled")
	}
}

func calendarBucket(start, end time.Time, label string) bucketDescriptor {
	startUTC := start.UTC()
	return bucketDescriptor{
		key:   startUTC.Format(time.RFC3339Nano),
		label: label,
		start: startUTC,
		end:   end.UTC(),
	}
}

func comparisonGroup(result database.StatisticsResult, dimension Dimension) (string, string) {
	var key string
	switch dimension {
	case DimensionNone:
		return "all", "All results"
	case DimensionTask:
		key = result.TaskID
	case DimensionInterface:
		key = result.Interface
	case DimensionSourceIP:
		key = result.SourceIP
	case DimensionProvider:
		key = string(result.Provider)
	case DimensionServer:
		key = result.ServerID
	case DimensionRouteProfile:
		key = result.RouteProfileID
	case DimensionPublicIP:
		key = result.PublicIP
	}
	if key == "" {
		return "", "Unassigned"
	}
	return key, key
}

type groupAccumulator struct {
	key     string
	label   string
	overall *bucketAccumulator
	buckets map[string]*bucketAccumulator
}

func (g *groupAccumulator) finish() Group {
	accumulators := make([]*bucketAccumulator, 0, len(g.buckets))
	for _, bucket := range g.buckets {
		accumulators = append(accumulators, bucket)
	}
	sort.Slice(accumulators, func(i, j int) bool {
		if accumulators[i].descriptor.start.Equal(accumulators[j].descriptor.start) {
			return accumulators[i].descriptor.resultID < accumulators[j].descriptor.resultID
		}
		return accumulators[i].descriptor.start.Before(accumulators[j].descriptor.start)
	})
	group := Group{Key: g.key, Label: g.label, Overall: g.overall.finish(), Buckets: make([]Bucket, 0, len(accumulators))}
	for _, accumulator := range accumulators {
		group.Buckets = append(group.Buckets, accumulator.finish())
	}
	return group
}

type bucketAccumulator struct {
	descriptor bucketDescriptor
	counts     Counts
	download   []float64
	upload     []float64
	latency    []float64
	jitter     []float64
	loss       []float64
	duration   []float64
}

func (b *bucketAccumulator) add(result database.StatisticsResult) {
	b.counts.Total++
	switch result.Status {
	case models.StatusSucceeded:
		b.counts.Successful++
	case models.StatusFailed:
		b.counts.Failed++
	case models.StatusSkipped:
		b.counts.Skipped++
	case models.StatusCancelled:
		b.counts.Cancelled++
	default:
		b.counts.Other++
	}
	// Execution duration is process-owned rather than provider-owned, so a
	// failed or cancelled run still contributes a meaningful duration sample.
	// Skipped work did not execute and is excluded.
	if result.ExecutionDurationMS >= 0 && (result.Status == models.StatusSucceeded || result.Status == models.StatusFailed || result.Status == models.StatusCancelled) {
		b.duration = append(b.duration, float64(result.ExecutionDurationMS))
	}
	// Provider error sentinels and partial failed-result values never enter
	// throughput or quality aggregates. A successful zero remains valid.
	if result.Status != models.StatusSucceeded {
		return
	}
	if throughputMetricCompatible(result, result.DownloadBitsPerSecond, false) {
		b.download = appendIntegerMetric(b.download, result.DownloadBitsPerSecond)
	}
	if throughputMetricCompatible(result, result.UploadBitsPerSecond, true) {
		b.upload = appendIntegerMetric(b.upload, result.UploadBitsPerSecond)
	}
	b.latency = appendFloatMetric(b.latency, result.LatencyMilliseconds)
	b.jitter = appendFloatMetric(b.jitter, result.JitterMilliseconds)
	b.loss = appendFloatMetric(b.loss, result.PacketLossPercent)
}

// throughputMetricCompatible prevents known pre-fix wire-unit/methodology
// values from contaminating throughput aggregates. The result itself, its
// outcome, duration, latency and audit data remain part of the report.
func throughputMetricCompatible(result database.StatisticsResult, value *int64, upload bool) bool {
	if result.Provider == models.ProviderCloudflare && upload && result.ProviderVersion == models.CloudflareLegacyUploadVersion {
		return false
	}
	if result.Provider == models.ProviderLibreSpeed &&
		!strings.Contains(result.ProviderVersion, models.LibreSpeedBitsMethodologyVersion) &&
		value != nil && *value >= 0 && *value < 100_000 {
		// Older MultiSpeed builds persisted LibreSpeed's numeric Mbit/s value
		// directly as bit/s. Such legacy values are always below this boundary;
		// current results carry an explicit unit-methodology marker.
		return false
	}
	return true
}

func appendIntegerMetric(destination []float64, value *int64) []float64 {
	if value == nil || *value < 0 {
		return destination
	}
	return append(destination, float64(*value))
}

func appendFloatMetric(destination []float64, value *float64) []float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return destination
	}
	return append(destination, *value)
}

func (b *bucketAccumulator) finish() Bucket {
	bucket := Bucket{
		Start:    b.descriptor.start,
		End:      b.descriptor.end,
		Label:    b.descriptor.label,
		ResultID: b.descriptor.resultID,
		Counts:   b.counts,
		Metrics: Metrics{
			DownloadBitsPerSecond:         summarize(b.download),
			UploadBitsPerSecond:           summarize(b.upload),
			LatencyMilliseconds:           summarize(b.latency),
			JitterMilliseconds:            summarize(b.jitter),
			PacketLossPercent:             summarize(b.loss),
			ExecutionDurationMilliseconds: summarize(b.duration),
		},
	}
	completed := b.counts.Successful + b.counts.Failed
	if completed > 0 {
		success := float64(b.counts.Successful) * 100 / float64(completed)
		failure := float64(b.counts.Failed) * 100 / float64(completed)
		bucket.SuccessRatePercent = &success
		bucket.FailureRatePercent = &failure
	}
	return bucket
}

func summarize(values []float64) Summary {
	if len(values) == 0 {
		return Summary{}
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	var sum float64
	for _, value := range ordered {
		sum += value
	}
	average := sum / float64(len(ordered))
	median := ordered[len(ordered)/2]
	if len(ordered)%2 == 0 {
		median = (ordered[len(ordered)/2-1] + ordered[len(ordered)/2]) / 2
	}
	p95Index := int(math.Ceil(0.95*float64(len(ordered)))) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	p95 := ordered[p95Index]
	var squaredDifferences float64
	for _, value := range ordered {
		difference := value - average
		squaredDifferences += difference * difference
	}
	standardDeviation := math.Sqrt(squaredDifferences / float64(len(ordered)))
	minimum, maximum := ordered[0], ordered[len(ordered)-1]
	return Summary{
		Count:             len(ordered),
		Minimum:           floatPointer(minimum),
		Maximum:           floatPointer(maximum),
		Average:           floatPointer(average),
		Median:            floatPointer(median),
		P95:               floatPointer(p95),
		StandardDeviation: floatPointer(standardDeviation),
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

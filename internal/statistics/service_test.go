package statistics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
)

type fakeResultSource struct {
	results []database.StatisticsResult
	filter  database.StatisticsFilter
	err     error
}

type generatedResultSource struct {
	count   int
	visited int
	result  func(int) database.StatisticsResult
}

func (f *generatedResultSource) WalkStatisticsResults(_ context.Context, _ database.StatisticsFilter, visit func(database.StatisticsResult) error) error {
	for index := 0; index < f.count; index++ {
		f.visited++
		if err := visit(f.result(index)); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeResultSource) WalkStatisticsResults(_ context.Context, filter database.StatisticsFilter, visit func(database.StatisticsResult) error) error {
	f.filter = filter
	if f.err != nil {
		return f.err
	}
	for _, result := range f.results {
		if err := visit(result); err != nil {
			return err
		}
	}
	return nil
}

func TestSummarizeMedianP95AndPopulationStandardDeviation(t *testing.T) {
	values := make([]float64, 20)
	for i := range values {
		values[i] = float64(i + 1)
	}
	summary := summarize(values)
	assertFloat(t, "minimum", summary.Minimum, 1)
	assertFloat(t, "maximum", summary.Maximum, 20)
	assertFloat(t, "average", summary.Average, 10.5)
	assertFloat(t, "median", summary.Median, 10.5)
	assertFloat(t, "p95", summary.P95, 19)
	assertFloat(t, "standard deviation", summary.StandardDeviation, math.Sqrt(33.25))
	if summary.Count != 20 {
		t.Fatalf("count = %d, want 20", summary.Count)
	}
	if values[0] != 1 || values[19] != 20 {
		t.Fatal("summarize mutated its input")
	}
}

func TestQueryExcludesFailedAndInvalidMetricSentinels(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	results := make([]database.StatisticsResult, 0, 23)
	for i := int64(1); i <= 20; i++ {
		value := i
		floating := float64(i)
		results = append(results, database.StatisticsResult{
			ID:                    string(rune('a' + i)),
			TaskID:                "task-a",
			Status:                models.StatusSucceeded,
			Timestamp:             from.Add(time.Duration(i) * time.Minute),
			DownloadBitsPerSecond: &value,
			UploadBitsPerSecond:   &value,
			LatencyMilliseconds:   &floating,
			JitterMilliseconds:    &floating,
			PacketLossPercent:     &floating,
			ExecutionDurationMS:   i,
		})
	}
	failedSentinel := int64(99999)
	failedFloat := 99999.0
	results = append(results,
		database.StatisticsResult{
			ID: "failed", TaskID: "task-a", Status: models.StatusFailed, Timestamp: from.Add(time.Hour),
			DownloadBitsPerSecond: &failedSentinel, LatencyMilliseconds: &failedFloat, ExecutionDurationMS: 99999,
		},
		database.StatisticsResult{ID: "skipped", TaskID: "task-a", Status: models.StatusSkipped, Timestamp: from.Add(2 * time.Hour)},
	)
	negative := int64(-1)
	notANumber := math.NaN()
	results = append(results, database.StatisticsResult{
		ID: "invalid", TaskID: "task-a", Status: models.StatusSucceeded, Timestamp: from.Add(3 * time.Hour),
		DownloadBitsPerSecond: &negative, LatencyMilliseconds: &notANumber, ExecutionDurationMS: -1,
	})

	service := New(&fakeResultSource{results: results})
	report, err := service.Query(context.Background(), Query{
		From: from, To: from.AddDate(0, 0, 1), Granularity: GranularityDay, ReportingTimezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalResults != 23 || len(report.Groups) != 1 || len(report.Groups[0].Buckets) != 1 {
		t.Fatalf("unexpected report shape: %#v", report)
	}
	bucket := report.Groups[0].Buckets[0]
	if bucket.Counts.Total != 23 || bucket.Counts.Successful != 21 || bucket.Counts.Failed != 1 || bucket.Counts.Skipped != 1 {
		t.Fatalf("unexpected counts: %+v", bucket.Counts)
	}
	if bucket.Metrics.DownloadBitsPerSecond.Count != 20 || bucket.Metrics.LatencyMilliseconds.Count != 20 ||
		bucket.Metrics.ExecutionDurationMilliseconds.Count != 21 {
		t.Fatalf("failed or invalid values entered metrics: %+v", bucket.Metrics)
	}
	assertFloat(t, "failed execution duration remains meaningful", bucket.Metrics.ExecutionDurationMilliseconds.Maximum, 99999)
	assertFloat(t, "download median", bucket.Metrics.DownloadBitsPerSecond.Median, 10.5)
	assertFloat(t, "download p95", bucket.Metrics.DownloadBitsPerSecond.P95, 19)
	assertFloat(t, "success rate", bucket.SuccessRatePercent, 21.0/22.0*100)
	assertFloat(t, "failure rate", bucket.FailureRatePercent, 1.0/22.0*100)
}

func TestQueryExcludesKnownLegacyThroughputWithoutDroppingAuditCounts(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	legacyCloudflareDownload, legacyCloudflareUpload := int64(170_000_000), int64(6_600_000_000)
	legacyLibreDownload, legacyLibreUpload := int64(77), int64(55)
	currentLibreDownload, currentLibreUpload := int64(130_000_000), int64(42_000_000)
	latency := 12.0
	results := []database.StatisticsResult{
		{ID: "cloudflare-v1", Provider: models.ProviderCloudflare, ProviderVersion: models.CloudflareLegacyUploadVersion, Status: models.StatusSucceeded, Timestamp: from.Add(time.Hour), DownloadBitsPerSecond: &legacyCloudflareDownload, UploadBitsPerSecond: &legacyCloudflareUpload, LatencyMilliseconds: &latency},
		{ID: "libre-legacy", Provider: models.ProviderLibreSpeed, ProviderVersion: "librespeed-cli v1", Status: models.StatusSucceeded, Timestamp: from.Add(2 * time.Hour), DownloadBitsPerSecond: &legacyLibreDownload, UploadBitsPerSecond: &legacyLibreUpload, LatencyMilliseconds: &latency},
		{ID: "libre-current", Provider: models.ProviderLibreSpeed, ProviderVersion: "librespeed-cli v1; " + models.LibreSpeedBitsMethodologyVersion, Status: models.StatusSucceeded, Timestamp: from.Add(3 * time.Hour), DownloadBitsPerSecond: &currentLibreDownload, UploadBitsPerSecond: &currentLibreUpload, LatencyMilliseconds: &latency},
	}
	report, err := New(&fakeResultSource{results: results}).Query(context.Background(), Query{From: from, To: from.AddDate(0, 0, 1), Granularity: GranularityDay, ReportingTimezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	bucket := report.Overall
	if report.TotalResults != 3 || bucket.Counts.Total != 3 || bucket.Counts.Successful != 3 {
		t.Fatalf("legacy results disappeared from audit counts: total=%d counts=%+v", report.TotalResults, bucket.Counts)
	}
	if bucket.Metrics.DownloadBitsPerSecond.Count != 2 || bucket.Metrics.UploadBitsPerSecond.Count != 1 || bucket.Metrics.LatencyMilliseconds.Count != 3 {
		t.Fatalf("unexpected compatibility filtering: %+v", bucket.Metrics)
	}
	assertFloat(t, "legacy Cloudflare download remains valid", bucket.Metrics.DownloadBitsPerSecond.Minimum, 130_000_000)
	assertFloat(t, "current LibreSpeed upload", bucket.Metrics.UploadBitsPerSecond.Average, 42_000_000)
}

func TestDayBucketsUseReportingTimezoneAcrossDST(t *testing.T) {
	from := time.Date(2025, time.March, 29, 0, 0, 0, 0, time.UTC)
	source := &fakeResultSource{results: []database.StatisticsResult{
		succeededResult("one", time.Date(2025, time.March, 29, 23, 30, 0, 0, time.UTC)),
		succeededResult("two", time.Date(2025, time.March, 30, 21, 30, 0, 0, time.UTC)),
		succeededResult("three", time.Date(2025, time.March, 30, 22, 30, 0, 0, time.UTC)),
	}}
	report, err := New(source).Query(context.Background(), Query{
		From: from, To: from.AddDate(0, 0, 3), Granularity: GranularityDay, ReportingTimezone: "Europe/Berlin",
	})
	if err != nil {
		t.Fatal(err)
	}
	buckets := report.Groups[0].Buckets
	if len(buckets) != 2 || buckets[0].Label != "2025-03-30" || buckets[0].Counts.Total != 2 || buckets[1].Label != "2025-03-31" {
		t.Fatalf("unexpected DST day buckets: %+v", buckets)
	}
	if duration := buckets[0].End.Sub(buckets[0].Start); duration != 23*time.Hour {
		t.Fatalf("DST day duration = %s, want 23h", duration)
	}
}

func TestISOWeekMonthAndYearBoundaries(t *testing.T) {
	results := []database.StatisticsResult{
		succeededResult("dec31", time.Date(2020, time.December, 31, 12, 0, 0, 0, time.UTC)),
		succeededResult("jan03", time.Date(2021, time.January, 3, 12, 0, 0, 0, time.UTC)),
		succeededResult("jan04", time.Date(2021, time.January, 4, 12, 0, 0, 0, time.UTC)),
	}
	from := time.Date(2020, time.December, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name        string
		granularity Granularity
		labels      []string
		counts      []int
	}{
		{"ISO week", GranularityISOWeek, []string{"2020-W53", "2021-W01"}, []int{2, 1}},
		{"month", GranularityMonth, []string{"2020-12", "2021-01"}, []int{1, 2}},
		{"year", GranularityYear, []string{"2020", "2021"}, []int{1, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := New(&fakeResultSource{results: results}).Query(context.Background(), Query{
				From: from, To: time.Date(2021, time.February, 1, 0, 0, 0, 0, time.UTC),
				Granularity: test.granularity, ReportingTimezone: "UTC",
			})
			if err != nil {
				t.Fatal(err)
			}
			buckets := report.Groups[0].Buckets
			if len(buckets) != len(test.labels) {
				t.Fatalf("got %d buckets, want %d", len(buckets), len(test.labels))
			}
			for i := range buckets {
				if buckets[i].Label != test.labels[i] || buckets[i].Counts.Total != test.counts[i] {
					t.Fatalf("bucket %d = %+v, want label %s count %d", i, buckets[i], test.labels[i], test.counts[i])
				}
			}
		})
	}
}

func TestRawCustomAndComparisonGrouping(t *testing.T) {
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	source := &fakeResultSource{results: []database.StatisticsResult{
		{ID: "b", TaskID: "task-b", Interface: "wan-b", Status: models.StatusSucceeded, Timestamp: from.Add(time.Hour)},
		{ID: "a", TaskID: "task-a", Interface: "wan-a", Status: models.StatusSucceeded, Timestamp: from.Add(time.Hour)},
	}}
	raw, err := New(source).Query(context.Background(), Query{
		From: from, To: from.AddDate(0, 0, 1), Granularity: GranularityRaw, ReportingTimezone: "UTC",
		GroupBy: DimensionTask, Filter: Filter{Interfaces: []string{" wan-a ", "wan-a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Groups) != 2 || raw.Groups[0].Key != "task-a" || raw.Groups[0].Buckets[0].ResultID != "a" || raw.Groups[1].Key != "task-b" {
		t.Fatalf("unexpected raw comparison groups: %+v", raw.Groups)
	}
	if len(source.filter.Interfaces) != 1 || source.filter.Interfaces[0] != "wan-a" {
		t.Fatalf("filter was not normalized and propagated: %+v", source.filter.Interfaces)
	}

	custom, err := New(&fakeResultSource{results: source.results}).Query(context.Background(), Query{
		From: from, To: from.AddDate(0, 0, 1), Granularity: GranularityCustom, ReportingTimezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(custom.Groups) != 1 || len(custom.Groups[0].Buckets) != 1 || custom.Groups[0].Buckets[0].Counts.Total != 2 {
		t.Fatalf("unexpected custom range aggregation: %+v", custom)
	}
}

func TestQueryBoundsMaterializedOutputBeforeAllocatingAnotherBucket(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	result := func(index int) database.StatisticsResult {
		return database.StatisticsResult{
			ID:        fmt.Sprintf("result-%05d", index),
			TaskID:    "task-a",
			Status:    models.StatusSkipped,
			Timestamp: from.Add(time.Duration(index) * time.Nanosecond),
		}
	}
	allowedResults := MaxStatisticsOutputPoints - 2 // Report.Overall + the one Group.Overall.
	allowedSource := &generatedResultSource{count: allowedResults, result: result}
	report, err := New(allowedSource).Query(context.Background(), Query{
		From: from, To: from.Add(time.Hour), Granularity: GranularityRaw, ReportingTimezone: "UTC", GroupBy: DimensionTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if points := statisticsOutputPoints(report); points != MaxStatisticsOutputPoints {
		t.Fatalf("output points=%d, want %d", points, MaxStatisticsOutputPoints)
	}
	if report.TotalResults != allowedResults {
		t.Fatalf("total results=%d, want %d", report.TotalResults, allowedResults)
	}

	overflowSource := &generatedResultSource{count: allowedResults + 1, result: result}
	_, err = New(overflowSource).Query(context.Background(), Query{
		From: from, To: from.Add(time.Hour), Granularity: GranularityRaw, ReportingTimezone: "UTC", GroupBy: DimensionTask,
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("overflow error=%v, want ErrOutputLimit", err)
	}
	if overflowSource.visited != allowedResults+1 {
		t.Fatalf("visited=%d, want early stop at %d", overflowSource.visited, allowedResults+1)
	}
}

func TestQueryBoundsComparisonGroupsAndDimensionBytes(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	groupsSource := &generatedResultSource{count: MaxStatisticsGroups + 1, result: func(index int) database.StatisticsResult {
		return database.StatisticsResult{
			ID: fmt.Sprintf("result-%04d", index), PublicIP: fmt.Sprintf("public-ip-%04d", index),
			Status: models.StatusSkipped, Timestamp: from,
		}
	}}
	_, err := New(groupsSource).Query(context.Background(), Query{
		From: from, To: from.Add(time.Hour), Granularity: GranularityCustom, ReportingTimezone: "UTC", GroupBy: DimensionPublicIP,
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("group overflow error=%v, want ErrOutputLimit", err)
	}
	if groupsSource.visited != MaxStatisticsGroups+1 {
		t.Fatalf("visited=%d, want early stop at %d", groupsSource.visited, MaxStatisticsGroups+1)
	}

	oversizedDimension := &generatedResultSource{count: 1, result: func(int) database.StatisticsResult {
		return database.StatisticsResult{ServerID: strings.Repeat("s", MaxStatisticsDimensionBytes+1), Status: models.StatusSkipped, Timestamp: from}
	}}
	_, err = New(oversizedDimension).Query(context.Background(), Query{
		From: from, To: from.Add(time.Hour), Granularity: GranularityCustom, ReportingTimezone: "UTC", GroupBy: DimensionServer,
	})
	if !errors.Is(err, ErrDimensionLimit) {
		t.Fatalf("dimension overflow error=%v, want ErrDimensionLimit", err)
	}
	if errors.Is(err, ErrOutputLimit) {
		t.Fatalf("dimension overflow error=%v also matched ErrOutputLimit", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d bytes", MaxStatisticsDimensionBytes)) {
		t.Fatalf("dimension overflow error=%q does not identify the byte limit", err)
	}
	if oversizedDimension.visited != 1 {
		t.Fatalf("oversized dimension visited=%d, want 1", oversizedDimension.visited)
	}
}

func TestQueryPreservesFiftyThousandRowLimitForCompactAggregates(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	source := &generatedResultSource{count: maxStatisticsResults, result: func(int) database.StatisticsResult {
		return database.StatisticsResult{TaskID: "task-a", Status: models.StatusSkipped, Timestamp: from}
	}}
	report, err := New(source).Query(context.Background(), Query{
		From: from, To: from.Add(time.Hour), Granularity: GranularityCustom, ReportingTimezone: "UTC", GroupBy: DimensionTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalResults != maxStatisticsResults || statisticsOutputPoints(report) != 3 {
		t.Fatalf("compact report results=%d points=%d", report.TotalResults, statisticsOutputPoints(report))
	}
}

func TestQueryReturnsExactFullRangeSummaries(t *testing.T) {
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	values := []int64{10, 20, 30, 999, 100}
	source := &fakeResultSource{results: []database.StatisticsResult{
		{ID: "a-one", Interface: "wan-a", Status: models.StatusSucceeded, Timestamp: from.Add(time.Hour), DownloadBitsPerSecond: &values[0]},
		{ID: "a-two", Interface: "wan-a", Status: models.StatusSucceeded, Timestamp: from.AddDate(0, 0, 1).Add(time.Hour), DownloadBitsPerSecond: &values[1]},
		{ID: "a-three", Interface: "wan-a", Status: models.StatusSucceeded, Timestamp: from.AddDate(0, 0, 1).Add(2 * time.Hour), DownloadBitsPerSecond: &values[2]},
		{ID: "a-failed", Interface: "wan-a", Status: models.StatusFailed, Timestamp: from.AddDate(0, 0, 1).Add(3 * time.Hour), DownloadBitsPerSecond: &values[3]},
		{ID: "b-one", Interface: "wan-b", Status: models.StatusSucceeded, Timestamp: from.Add(time.Hour), DownloadBitsPerSecond: &values[4]},
	}}

	report, err := New(source).Query(context.Background(), Query{
		From: from, To: from.AddDate(0, 0, 2), Granularity: GranularityDay,
		ReportingTimezone: "UTC", GroupBy: DimensionInterface,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall.Counts.Total != 5 || report.Overall.Counts.Successful != 4 || report.Overall.Counts.Failed != 1 {
		t.Fatalf("unexpected report-wide counts: %+v", report.Overall.Counts)
	}
	assertFloat(t, "report-wide exact average", report.Overall.Metrics.DownloadBitsPerSecond.Average, 40)
	assertFloat(t, "report-wide success rate", report.Overall.SuccessRatePercent, 80)
	if !report.Overall.Start.Equal(from) || !report.Overall.End.Equal(from.AddDate(0, 0, 2)) {
		t.Fatalf("report-wide range = [%s, %s)", report.Overall.Start, report.Overall.End)
	}

	if len(report.Groups) != 2 || report.Groups[0].Key != "wan-a" || report.Groups[1].Key != "wan-b" {
		t.Fatalf("unexpected groups: %+v", report.Groups)
	}
	wanA := report.Groups[0]
	if wanA.Overall.Counts.Total != 4 || wanA.Overall.Counts.Successful != 3 || wanA.Overall.Counts.Failed != 1 {
		t.Fatalf("unexpected wan-a counts: %+v", wanA.Overall.Counts)
	}
	assertFloat(t, "wan-a exact average", wanA.Overall.Metrics.DownloadBitsPerSecond.Average, 20)
	assertFloat(t, "wan-a success rate", wanA.Overall.SuccessRatePercent, 75)
	if len(wanA.Buckets) != 2 {
		t.Fatalf("wan-a buckets=%d, want 2", len(wanA.Buckets))
	}
	assertFloat(t, "wan-a day one average", wanA.Buckets[0].Metrics.DownloadBitsPerSecond.Average, 10)
	assertFloat(t, "wan-a day two average", wanA.Buckets[1].Metrics.DownloadBitsPerSecond.Average, 25)
	if averagedBucketAverages := (*wanA.Buckets[0].Metrics.DownloadBitsPerSecond.Average + *wanA.Buckets[1].Metrics.DownloadBitsPerSecond.Average) / 2; averagedBucketAverages == *wanA.Overall.Metrics.DownloadBitsPerSecond.Average {
		t.Fatal("test fixture does not distinguish an exact sample aggregate from an unweighted average of bucket averages")
	}
}

func TestQueryValidationAndEmptyMetrics(t *testing.T) {
	from := time.Now().UTC()
	service := New(&fakeResultSource{})
	for _, query := range []Query{
		{From: from, To: from, Granularity: GranularityDay},
		{From: from, To: from.Add(time.Hour), Granularity: "quarter"},
		{From: from, To: from.Add(time.Hour), Granularity: GranularityDay, ReportingTimezone: "Mars/Olympus"},
		{From: from, To: from.Add(time.Hour), Granularity: GranularityDay, GroupBy: "invalid"},
	} {
		if _, err := service.Query(context.Background(), query); err == nil {
			t.Fatalf("Query(%+v) unexpectedly succeeded", query)
		}
	}
	if summary := summarize(nil); summary.Count != 0 || summary.Minimum != nil || summary.P95 != nil {
		t.Fatalf("empty summary should contain null values: %+v", summary)
	}
}

func succeededResult(id string, timestamp time.Time) database.StatisticsResult {
	return database.StatisticsResult{ID: id, TaskID: "task", Status: models.StatusSucceeded, Timestamp: timestamp}
}

func statisticsOutputPoints(report Report) int {
	points := 1 // report.Overall
	for _, group := range report.Groups {
		points++ // group.Overall
		points += len(group.Buckets)
	}
	return points
}

func assertFloat(t *testing.T, name string, actual *float64, expected float64) {
	t.Helper()
	if actual == nil || math.Abs(*actual-expected) > 1e-9 {
		t.Fatalf("%s = %v, want %.12f", name, actual, expected)
	}
}

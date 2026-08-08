package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
	statisticsservice "github.com/dude2k/MultiSpeed/internal/statistics"
)

type apiGeneratedStatisticsSource struct {
	count   int
	visited int
	result  func(int) database.StatisticsResult
}

func (f *apiGeneratedStatisticsSource) WalkStatisticsResults(_ context.Context, _ database.StatisticsFilter, visit func(database.StatisticsResult) error) error {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	for index := 0; index < f.count; index++ {
		f.visited++
		result := database.StatisticsResult{
			ID: fmt.Sprintf("result-%05d", index), TaskID: "task-a", Status: models.StatusSkipped,
			Timestamp: from.Add(time.Duration(index) * time.Nanosecond),
		}
		if f.result != nil {
			result = f.result(index)
		}
		if err := visit(result); err != nil {
			return err
		}
	}
	return nil
}

func TestParseStatisticsQueryCountsInterfaceAliasesTogether(t *testing.T) {
	values := validStatisticsQueryValues()
	for index := 0; index < 100; index++ {
		values.Add("interface", fmt.Sprintf("wan-%03d", index))
	}
	for index := 100; index < maxStatisticsFilterValues; index++ {
		values.Add("interfaceName", fmt.Sprintf("wan-%03d", index))
	}
	query, err := (&Server{}).parseStatisticsQuery(statisticsRequest(values))
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Filter.Interfaces) != maxStatisticsFilterValues {
		t.Fatalf("interface filters=%d, want %d", len(query.Filter.Interfaces), maxStatisticsFilterValues)
	}
}

func TestParseStatisticsQueryRejectsOversizedFilterListsAndValues(t *testing.T) {
	tests := map[string]func(url.Values){
		"comma-separated list": func(values url.Values) {
			items := make([]string, maxStatisticsFilterValues+1)
			for index := range items {
				items[index] = fmt.Sprintf("task-%03d", index)
			}
			values.Set("taskId", strings.Join(items, ","))
		},
		"combined interface aliases": func(values url.Values) {
			for index := 0; index < 100; index++ {
				values.Add("interface", fmt.Sprintf("wan-%03d", index))
			}
			for index := 100; index <= maxStatisticsFilterValues; index++ {
				values.Add("interfaceName", fmt.Sprintf("wan-%03d", index))
			}
		},
		"oversized value": func(values url.Values) {
			values.Set("serverId", strings.Repeat("s", maxStatisticsFilterValueLength+1))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			values := validStatisticsQueryValues()
			mutate(values)
			if _, err := (&Server{}).parseStatisticsQuery(statisticsRequest(values)); err == nil {
				t.Fatal("invalid statistics filter unexpectedly succeeded")
			}
		})
	}
}

func TestStatisticsHandlerReturnsBadRequestForTooManyFilterValues(t *testing.T) {
	values := validStatisticsQueryValues()
	for index := 0; index <= maxStatisticsFilterValues; index++ {
		values.Add("taskId", fmt.Sprintf("task-%03d", index))
	}
	recorder := httptest.NewRecorder()
	(&Server{}).statistics(recorder, statisticsRequest(values))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "INVALID_STATISTICS_QUERY" {
		t.Fatalf("error code=%q", envelope.Error.Code)
	}
}

func TestStatisticsHandlerMapsOutputLimitToActionable413(t *testing.T) {
	source := &apiGeneratedStatisticsSource{count: statisticsservice.MaxStatisticsOutputPoints}
	server := &Server{statsService: statisticsservice.New(source)}
	values := validStatisticsQueryValues()
	values.Set("granularity", "raw")
	values.Set("groupBy", "task")
	recorder := httptest.NewRecorder()
	server.statistics(recorder, statisticsRequest(values))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "STATISTICS_OUTPUT_LIMIT_EXCEEDED" {
		t.Fatalf("error code=%q", envelope.Error.Code)
	}
	for _, guidance := range []string{"coarser granularity", "shorter range", "narrower filters"} {
		if !strings.Contains(envelope.Error.Message, guidance) {
			t.Fatalf("error message %q does not contain %q", envelope.Error.Message, guidance)
		}
	}
	if source.visited >= source.count {
		t.Fatalf("source visited all %d rows instead of stopping at the output cap", source.count)
	}
}

func TestStatisticsHandlerMapsDimensionLimitToActionable413(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	source := &apiGeneratedStatisticsSource{count: 1, result: func(int) database.StatisticsResult {
		return database.StatisticsResult{
			ID: "result-oversized-dimension", ServerID: strings.Repeat("s", statisticsservice.MaxStatisticsDimensionBytes+1),
			Status: models.StatusSkipped, Timestamp: from,
		}
	}}
	server := &Server{statsService: statisticsservice.New(source)}
	values := validStatisticsQueryValues()
	values.Set("granularity", "custom")
	values.Set("groupBy", "server")
	recorder := httptest.NewRecorder()
	server.statistics(recorder, statisticsRequest(values))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "STATISTICS_DIMENSION_LIMIT_EXCEEDED" {
		t.Fatalf("error code=%q", envelope.Error.Code)
	}
	for _, guidance := range []string{"longer than 512 bytes", "another groupBy dimension", "narrow the query"} {
		if !strings.Contains(envelope.Error.Message, guidance) {
			t.Fatalf("error message %q does not contain %q", envelope.Error.Message, guidance)
		}
	}
	if strings.Contains(envelope.Error.Message, "5,000 output points") {
		t.Fatalf("dimension error reused the output-cardinality message: %q", envelope.Error.Message)
	}
	if source.visited != 1 {
		t.Fatalf("source visited=%d, want 1", source.visited)
	}
}

func validStatisticsQueryValues() url.Values {
	return url.Values{
		"from":     {"2026-08-01T00:00:00Z"},
		"to":       {"2026-08-02T00:00:00Z"},
		"timezone": {"UTC"},
	}
}

func statisticsRequest(values url.Values) *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/statistics?"+values.Encode(), nil)
}

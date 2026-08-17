package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type httpMetrics struct {
	handler  http.Handler
	inFlight prometheus.Gauge
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newHTTPMetrics(store *database.Store) *httpMetrics {
	registry := prometheus.NewRegistry()
	metrics := &httpMetrics{
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multispeed",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Current number of HTTP requests being served.",
		}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multispeed",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests by method, route, and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multispeed",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds by method and route.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	registry.MustRegister(metrics.inFlight, metrics.requests, metrics.duration)
	registry.MustRegister(newApplicationStateCollector(store))
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics.handler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
	return metrics
}

func (m *httpMetrics) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		m.inFlight.Inc()
		defer m.inFlight.Dec()
		recorder := &responseRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		route := chi.RouteContext(request.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		m.requests.WithLabelValues(request.Method, route, strconv.Itoa(recorder.status)).Inc()
		m.duration.WithLabelValues(request.Method, route).Observe(time.Since(started).Seconds())
	})
}

type applicationStateCollector struct {
	store      *database.Store
	tasks      *prometheus.Desc
	results    *prometheus.Desc
	activeRuns *prometheus.Desc
}

func newApplicationStateCollector(store *database.Store) *applicationStateCollector {
	return &applicationStateCollector{
		store:      store,
		tasks:      prometheus.NewDesc("multispeed_tasks", "Current number of persisted speed-test tasks.", nil, nil),
		results:    prometheus.NewDesc("multispeed_results", "Current number of persisted result records.", nil, nil),
		activeRuns: prometheus.NewDesc("multispeed_active_runs", "Current number of queued, validating, or running tests.", nil, nil),
	}
}

func (c *applicationStateCollector) Describe(descriptions chan<- *prometheus.Desc) {
	descriptions <- c.tasks
	descriptions <- c.results
	descriptions <- c.activeRuns
}

func (c *applicationStateCollector) Collect(metrics chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tasks, results, activeRuns, err := c.store.Counts(ctx)
	if err != nil {
		metrics <- prometheus.NewInvalidMetric(c.tasks, err)
		return
	}
	metrics <- prometheus.MustNewConstMetric(c.tasks, prometheus.GaugeValue, float64(tasks))
	metrics <- prometheus.MustNewConstMetric(c.results, prometheus.GaugeValue, float64(results))
	metrics <- prometheus.MustNewConstMetric(c.activeRuns, prometheus.GaugeValue, float64(activeRuns))
}

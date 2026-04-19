package observability

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

type Recorder interface {
	HTTPMiddleware() gin.HandlerFunc
	Handler() http.Handler
	RecordRedisOperation(operation string, purpose string, start time.Time, err error)
	RecordAuthAttempt(status string, reason string)
	RecordTenantOperation(operation string, status string, start time.Time)
	RecordRateLimitDecision(status string, reason string)
	RecordAPIKeyCreation(status string, reason string)
}

type Prometheus struct {
	registry                       *prometheus.Registry
	httpRequestsTotal              *prometheus.CounterVec
	httpRequestDurationSeconds     *prometheus.HistogramVec
	httpRequestsInFlight           prometheus.Gauge
	redisOperationsTotal           *prometheus.CounterVec
	redisOperationDurationSeconds  *prometheus.HistogramVec
	redisErrorsTotal               *prometheus.CounterVec
	authAttemptsTotal              *prometheus.CounterVec
	tenantOperationsTotal          *prometheus.CounterVec
	tenantOperationDurationSeconds *prometheus.HistogramVec
	rateLimitRequestsTotal         *prometheus.CounterVec
	apiKeyCreationsTotal           *prometheus.CounterVec
}

func NewPrometheus(namespace string) *Prometheus {
	registry := prometheus.NewRegistry()

	p := &Prometheus{
		registry: registry,
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests handled by the server.",
			},
			[]string{"method", "route", "status"},
		),
		httpRequestDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"method", "route", "status"},
		),
		httpRequestsInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "http_requests_in_flight",
				Help:      "Current number of HTTP requests being processed.",
			},
		),
		redisOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "redis_operations_total",
				Help:      "Total number of Redis operations.",
			},
			[]string{"operation", "purpose", "status"},
		),
		redisOperationDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "redis_operation_duration_seconds",
				Help:      "Redis operation duration in seconds.",
				Buckets:   []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
			[]string{"operation", "purpose", "status"},
		),
		redisErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "redis_errors_total",
				Help:      "Total number of Redis operation errors.",
			},
			[]string{"operation", "purpose", "error"},
		),
		authAttemptsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "auth_attempts_total",
				Help:      "Total number of API authentication attempts.",
			},
			[]string{"status", "reason"},
		),
		tenantOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tenant_operations_total",
				Help:      "Total number of tenant data operations.",
			},
			[]string{"operation", "status"},
		),
		tenantOperationDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "tenant_operation_duration_seconds",
				Help:      "Tenant data operation duration in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"operation", "status"},
		),
		rateLimitRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "rate_limit_requests_total",
				Help:      "Total number of rate limit decisions.",
			},
			[]string{"status", "reason"},
		),
		apiKeyCreationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "api_key_creations_total",
				Help:      "Total number of API key creation attempts.",
			},
			[]string{"status", "reason"},
		),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		p.httpRequestsTotal,
		p.httpRequestDurationSeconds,
		p.httpRequestsInFlight,
		p.redisOperationsTotal,
		p.redisOperationDurationSeconds,
		p.redisErrorsTotal,
		p.authAttemptsTotal,
		p.tenantOperationsTotal,
		p.tenantOperationDurationSeconds,
		p.rateLimitRequestsTotal,
		p.apiKeyCreationsTotal,
	)

	return p
}

func (p *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}

func (p *Prometheus) HTTPMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		p.httpRequestsInFlight.Inc()
		defer p.httpRequestsInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		status := strconv.Itoa(c.Writer.Status())

		p.httpRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		p.httpRequestDurationSeconds.WithLabelValues(c.Request.Method, route, status).Observe(time.Since(start).Seconds())
	}
}

func (p *Prometheus) RecordRedisOperation(operation string, purpose string, start time.Time, err error) {
	status := "success"
	if errors.Is(err, redis.Nil) {
		status = "not_found"
	} else if err != nil {
		status = "error"
		p.redisErrorsTotal.WithLabelValues(operation, purpose, redisErrorKind(err)).Inc()
	}

	p.redisOperationsTotal.WithLabelValues(operation, purpose, status).Inc()
	p.redisOperationDurationSeconds.WithLabelValues(operation, purpose, status).Observe(time.Since(start).Seconds())
}

func (p *Prometheus) RecordAuthAttempt(status string, reason string) {
	p.authAttemptsTotal.WithLabelValues(status, reason).Inc()
}

func (p *Prometheus) RecordTenantOperation(operation string, status string, start time.Time) {
	p.tenantOperationsTotal.WithLabelValues(operation, status).Inc()
	p.tenantOperationDurationSeconds.WithLabelValues(operation, status).Observe(time.Since(start).Seconds())
}

func (p *Prometheus) RecordRateLimitDecision(status string, reason string) {
	p.rateLimitRequestsTotal.WithLabelValues(status, reason).Inc()
}

func (p *Prometheus) RecordAPIKeyCreation(status string, reason string) {
	p.apiKeyCreationsTotal.WithLabelValues(status, reason).Inc()
}

func redisErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "redis_error"
	}
}

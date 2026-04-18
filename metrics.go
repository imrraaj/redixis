package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

const metricsNamespace = "redixis"

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests handled by the server.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "route", "status"},
	)

	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_in_flight",
			Help:      "Current number of HTTP requests being processed.",
		},
	)

	redisOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "redis_operations_total",
			Help:      "Total number of Redis operations.",
		},
		[]string{"operation", "purpose", "status"},
	)

	redisOperationDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "redis_operation_duration_seconds",
			Help:      "Redis operation duration in seconds.",
			Buckets:   []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"operation", "purpose", "status"},
	)

	redisErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "redis_errors_total",
			Help:      "Total number of Redis operation errors.",
		},
		[]string{"operation", "purpose", "error"},
	)

	authAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "auth_attempts_total",
			Help:      "Total number of API authentication attempts.",
		},
		[]string{"status", "reason"},
	)

	tenantOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "tenant_operations_total",
			Help:      "Total number of tenant data operations.",
		},
		[]string{"operation", "status"},
	)

	tenantOperationDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "tenant_operation_duration_seconds",
			Help:      "Tenant data operation duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"operation", "status"},
	)

	rateLimitRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "rate_limit_requests_total",
			Help:      "Total number of rate limit decisions.",
		},
		[]string{"status", "reason"},
	)

	apiKeyCreationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "api_key_creations_total",
			Help:      "Total number of API key creation attempts.",
		},
		[]string{"status", "reason"},
	)
)

func init() {
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDurationSeconds,
		httpRequestsInFlight,
		redisOperationsTotal,
		redisOperationDurationSeconds,
		redisErrorsTotal,
		authAttemptsTotal,
		tenantOperationsTotal,
		tenantOperationDurationSeconds,
		rateLimitRequestsTotal,
		apiKeyCreationsTotal,
	)
}

func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		status := strconv.Itoa(c.Writer.Status())

		httpRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		httpRequestDurationSeconds.WithLabelValues(c.Request.Method, route, status).Observe(time.Since(start).Seconds())
	}
}

func recordRedisOperation(operation string, purpose string, start time.Time, err error) {
	status := "success"
	if errors.Is(err, redis.Nil) {
		status = "not_found"
	} else if err != nil {
		status = "error"
		redisErrorsTotal.WithLabelValues(operation, purpose, redisErrorKind(err)).Inc()
	}

	redisOperationsTotal.WithLabelValues(operation, purpose, status).Inc()
	redisOperationDurationSeconds.WithLabelValues(operation, purpose, status).Observe(time.Since(start).Seconds())
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

func recordAuthAttempt(status string, reason string) {
	authAttemptsTotal.WithLabelValues(status, reason).Inc()
}

func recordTenantOperation(operation string, status string, start time.Time) {
	tenantOperationsTotal.WithLabelValues(operation, status).Inc()
	tenantOperationDurationSeconds.WithLabelValues(operation, status).Observe(time.Since(start).Seconds())
}

func recordRateLimitDecision(status string, reason string) {
	rateLimitRequestsTotal.WithLabelValues(status, reason).Inc()
}

func recordAPIKeyCreation(status string, reason string) {
	apiKeyCreationsTotal.WithLabelValues(status, reason).Inc()
}

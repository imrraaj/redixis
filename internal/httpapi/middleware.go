package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"redixis/internal/observability"
	"redixis/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	contextTenantID = "tenant_id"
	contextCommand  = "command"
)

var allowedCommands = map[string]struct{}{
	"GET":  {},
	"SET":  {},
	"DEL":  {},
	"INCR": {},
	"DECR": {},
	"MGET": {},
	"MSET": {},
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if c.Request.URL.Path == "/metrics" {
			return
		}

		logger.InfoContext(
			c.Request.Context(),
			"http_request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func commandGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenant_id")
		if !security.ValidateTenantID(tenantID) {
			writeBadRequest(c, "tenant id must be 12-32 alphanumeric characters")
			return
		}

		command := strings.ToUpper(strings.TrimSpace(c.Param("command")))
		if _, ok := allowedCommands[command]; !ok {
			writeError(c, http.StatusBadRequest, "unsupported_command", "command must be one of GET, SET, DEL, INCR, DECR, MGET, MSET")
			return
		}

		c.Set(contextTenantID, tenantID)
		c.Set(contextCommand, command)
		c.Next()
	}
}

func apiKeyAuth(authHandler *AuthHandler, metrics observability.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetString(contextTenantID)
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			apiKey = c.GetHeader("X-Authorization")
		}
		if apiKey == "" {
			metrics.RecordAuthAttempt("failure", "missing_api_key")
			writeError(c, http.StatusForbidden, "forbidden", "missing API key")
			return
		}

		if !authHandler.AuthenticateAPIKey(c.Request.Context(), tenantID, apiKey) {
			metrics.RecordAuthAttempt("failure", "invalid_credentials")
			writeError(c, http.StatusForbidden, "forbidden", "invalid API key")
			return
		}

		metrics.RecordAuthAttempt("success", "ok")
		c.Next()
	}
}

func rateLimit(redisClient *redis.Client, timeout time.Duration, metrics observability.Recorder, limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}

		tenantID := c.GetString(contextTenantID)
		ctx, cancel := redisContext(c, timeout)
		defer cancel()

		bucket := time.Now().UTC().Unix() / int64(time.Minute.Seconds())
		key := fmt.Sprintf("rate:%s:%d", tenantID, bucket)

		redisStart := time.Now()
		count, err := redisClient.Incr(ctx, key).Result()
		metrics.RecordRedisOperation("INCR", "rate_limit", redisStart, err)
		if err != nil {
			metrics.RecordRateLimitDecision("error", "redis_error")
			writeError(c, http.StatusServiceUnavailable, "rate_limit_unavailable", "rate limiter is unavailable")
			return
		}

		if count == 1 {
			redisStart = time.Now()
			err = redisClient.Expire(ctx, key, 2*time.Minute).Err()
			metrics.RecordRedisOperation("EXPIRE", "rate_limit", redisStart, err)
			if err != nil {
				metrics.RecordRateLimitDecision("error", "redis_error")
				writeError(c, http.StatusServiceUnavailable, "rate_limit_unavailable", "rate limiter is unavailable")
				return
			}
		}

		if count > limit {
			metrics.RecordRateLimitDecision("rejected", "limit_exceeded")
			writeError(c, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}

		metrics.RecordRateLimitDecision("allowed", "ok")
		c.Next()
	}
}

func redisContext(c *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(c.Request.Context())
	}
	return context.WithTimeout(c.Request.Context(), timeout)
}

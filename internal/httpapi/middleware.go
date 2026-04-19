package httpapi

import (
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

// rateLimitScript atomically increments the per-tenant per-minute counter and
// sets a 2-minute TTL on the first increment. Doing both in a single Lua
// eval prevents the key from persisting forever if the server crashes between
// an INCR and the subsequent EXPIRE.
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

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
		result, err := rateLimitScript.Run(ctx, redisClient, []string{key}, 120).Int64()
		metrics.RecordRedisOperation("EVAL", "rate_limit", redisStart, err)
		if err != nil {
			metrics.RecordRateLimitDecision("error", "redis_error")
			writeError(c, http.StatusServiceUnavailable, "rate_limit_unavailable", "rate limiter is unavailable")
			return
		}

		if result > limit {
			metrics.RecordRateLimitDecision("rejected", "limit_exceeded")
			writeError(c, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}

		metrics.RecordRateLimitDecision("allowed", "ok")
		c.Next()
	}
}

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"redixis/internal/observability"
	"redixis/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var errInvalidTenantKey = errors.New("invalid tenant key")

type TenantHandler struct {
	redis   *redis.Client
	timeout time.Duration
	metrics observability.Recorder
}

type keyRequest struct {
	Key string `json:"key" binding:"required"`
}

type setRequest struct {
	Key        string          `json:"key" binding:"required"`
	Value      json.RawMessage `json:"value" binding:"required"`
	TTLSeconds int64           `json:"ttl_seconds"`
}

type keysRequest struct {
	Keys []string `json:"keys"`
}

type msetRequest struct {
	Items map[string]json.RawMessage `json:"items" binding:"required"`
}

func NewTenantHandler(redisClient *redis.Client, timeout time.Duration, metrics observability.Recorder) *TenantHandler {
	return &TenantHandler{
		redis:   redisClient,
		timeout: timeout,
		metrics: metrics,
	}
}

func (h *TenantHandler) HandleCommand(c *gin.Context) {
	tenantID := c.GetString(contextTenantID)
	command := c.GetString(contextCommand)
	start := time.Now()

	switch command {
	case "GET":
		h.handleGet(c, tenantID, start)
	case "SET":
		h.handleSet(c, tenantID, start)
	case "DEL":
		h.handleDel(c, tenantID, start)
	case "INCR":
		h.handleIncr(c, tenantID, start)
	case "DECR":
		h.handleDecr(c, tenantID, start)
	case "MGET":
		h.handleMGet(c, tenantID, start)
	case "MSET":
		h.handleMSet(c, tenantID, start)
	}
}

func (h *TenantHandler) handleGet(c *gin.Context, tenantID string, start time.Time) {
	var req keyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("GET", start, c, "key is required")
		return
	}

	redisKey, err := tenantKeyName(tenantID, req.Key)
	if err != nil {
		h.recordBadRequest("GET", start, c, "invalid key")
		return
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	value, err := h.redis.Get(ctx, redisKey).Result()
	h.metrics.RecordRedisOperation("GET", "tenant_get", redisStart, err)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			h.metrics.RecordTenantOperation("GET", "not_found", start)
			writeError(c, http.StatusNotFound, "not_found", "key not found")
			return
		}
		h.recordStoreError("GET", start, c)
		return
	}

	h.metrics.RecordTenantOperation("GET", "success", start)
	c.JSON(http.StatusOK, gin.H{"value": value})
}

func (h *TenantHandler) handleSet(c *gin.Context, tenantID string, start time.Time) {
	var req setRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("SET", start, c, "key and value are required")
		return
	}

	redisKey, err := tenantKeyName(tenantID, req.Key)
	if err != nil {
		h.recordBadRequest("SET", start, c, "invalid key")
		return
	}

	value, err := rawValueToString(req.Value)
	if err != nil {
		h.recordBadRequest("SET", start, c, "value must be valid JSON")
		return
	}

	ttl := req.TTLSeconds
	if ttl < 0 {
		h.recordBadRequest("SET", start, c, "ttl_seconds cannot be negative")
		return
	}

	var duration time.Duration
	if ttl > 0 {
		duration = time.Duration(ttl) * time.Second
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	err = h.redis.Set(ctx, redisKey, value, duration).Err()
	h.metrics.RecordRedisOperation("SET", "tenant_set", redisStart, err)
	if err != nil {
		h.recordStoreError("SET", start, c)
		return
	}

	h.metrics.RecordTenantOperation("SET", "success", start)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *TenantHandler) handleDel(c *gin.Context, tenantID string, start time.Time) {
	var req keysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("DEL", start, c, "keys array is required")
		return
	}

	redisKeys, err := tenantKeyNames(tenantID, req.Keys)
	if err != nil {
		h.recordBadRequest("DEL", start, c, "invalid key")
		return
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	deleted, err := h.redis.Del(ctx, redisKeys...).Result()
	h.metrics.RecordRedisOperation("DEL", "tenant_delete", redisStart, err)
	if err != nil {
		h.recordStoreError("DEL", start, c)
		return
	}

	h.metrics.RecordTenantOperation("DEL", "success", start)
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

func (h *TenantHandler) handleIncr(c *gin.Context, tenantID string, start time.Time) {
	var req keyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("INCR", start, c, "key is required")
		return
	}

	redisKey, err := tenantKeyName(tenantID, req.Key)
	if err != nil {
		h.recordBadRequest("INCR", start, c, "invalid key")
		return
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	value, err := h.redis.Incr(ctx, redisKey).Result()
	h.metrics.RecordRedisOperation("INCR", "tenant_incr", redisStart, err)
	if err != nil {
		h.recordStoreError("INCR", start, c)
		return
	}

	h.metrics.RecordTenantOperation("INCR", "success", start)
	c.JSON(http.StatusOK, gin.H{"value": value})
}

func (h *TenantHandler) handleDecr(c *gin.Context, tenantID string, start time.Time) {
	var req keyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("DECR", start, c, "key is required")
		return
	}

	redisKey, err := tenantKeyName(tenantID, req.Key)
	if err != nil {
		h.recordBadRequest("DECR", start, c, "invalid key")
		return
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	value, err := h.redis.Decr(ctx, redisKey).Result()
	h.metrics.RecordRedisOperation("DECR", "tenant_decr", redisStart, err)
	if err != nil {
		h.recordStoreError("DECR", start, c)
		return
	}

	h.metrics.RecordTenantOperation("DECR", "success", start)
	c.JSON(http.StatusOK, gin.H{"value": value})
}

func (h *TenantHandler) handleMGet(c *gin.Context, tenantID string, start time.Time) {
	var req keysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("MGET", start, c, "keys array is required")
		return
	}

	redisKeys, err := tenantKeyNames(tenantID, req.Keys)
	if err != nil {
		h.recordBadRequest("MGET", start, c, "invalid key")
		return
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	values, err := h.redis.MGet(ctx, redisKeys...).Result()
	h.metrics.RecordRedisOperation("MGET", "tenant_mget", redisStart, err)
	if err != nil {
		h.recordStoreError("MGET", start, c)
		return
	}

	h.metrics.RecordTenantOperation("MGET", "success", start)
	c.JSON(http.StatusOK, gin.H{"values": values})
}

func (h *TenantHandler) handleMSet(c *gin.Context, tenantID string, start time.Time) {
	var req msetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.recordBadRequest("MSET", start, c, "items object is required")
		return
	}
	if len(req.Items) == 0 {
		h.recordBadRequest("MSET", start, c, "items object cannot be empty")
		return
	}

	items := make(map[string]interface{}, len(req.Items))
	for key, rawValue := range req.Items {
		redisKey, err := tenantKeyName(tenantID, key)
		if err != nil {
			h.recordBadRequest("MSET", start, c, "invalid key")
			return
		}

		value, err := rawValueToString(rawValue)
		if err != nil {
			h.recordBadRequest("MSET", start, c, "all item values must be valid JSON")
			return
		}
		items[redisKey] = value
	}

	ctx, cancel := redisContext(c, h.timeout)
	defer cancel()

	redisStart := time.Now()
	err := h.redis.MSet(ctx, items).Err()
	h.metrics.RecordRedisOperation("MSET", "tenant_mset", redisStart, err)
	if err != nil {
		h.recordStoreError("MSET", start, c)
		return
	}

	h.metrics.RecordTenantOperation("MSET", "success", start)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *TenantHandler) recordBadRequest(operation string, start time.Time, c *gin.Context, message string) {
	h.metrics.RecordTenantOperation(operation, "bad_request", start)
	writeBadRequest(c, message)
}

func (h *TenantHandler) recordStoreError(operation string, start time.Time, c *gin.Context) {
	h.metrics.RecordTenantOperation(operation, "redis_error", start)
	writeError(c, http.StatusServiceUnavailable, "redis_error", "tenant redis operation failed")
}

func tenantKeyNames(tenantID string, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, errInvalidTenantKey
	}

	redisKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		redisKey, err := tenantKeyName(tenantID, key)
		if err != nil {
			return nil, err
		}
		redisKeys = append(redisKeys, redisKey)
	}
	return redisKeys, nil
}

func tenantKeyName(tenantID string, key string) (string, error) {
	key = strings.TrimSpace(key)
	if !security.ValidateTenantID(tenantID) || key == "" || len(key) > 512 {
		return "", errInvalidTenantKey
	}
	return "tenant:{" + tenantID + "}:" + key, nil
}

func rawValueToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("missing value: raw JSON is empty")
	}

	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}

	if !json.Valid(raw) {
		return "", fmt.Errorf("invalid JSON value: %s", string(raw))
	}
	return string(raw), nil
}

package httpapi

import (
	"context"
	"net/http"
	"time"

	"redixis/internal/observability"
	"redixis/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const authKeyPrefix = "auth:"

func authKey(tenantID string) string {
	return authKeyPrefix + tenantID
}

type AuthHandler struct {
	client  *redis.Client
	timeout time.Duration
	metrics observability.Recorder
}

func NewAuthHandler(client *redis.Client, timeout time.Duration, metrics observability.Recorder) *AuthHandler {
	return &AuthHandler{
		client:  client,
		timeout: timeout,
		metrics: metrics,
	}
}

func (h *AuthHandler) CreateAccount(c *gin.Context) {
	tenantID, err := security.GenerateTenantID()
	if err != nil {
		h.metrics.RecordAPIKeyCreation("failure", "generate_error")
		writeError(c, http.StatusInternalServerError, "account_create_failed", "could not create account")
		return
	}

	apiKey, err := security.GenerateAPIKey()
	if err != nil {
		h.metrics.RecordAPIKeyCreation("failure", "generate_error")
		writeError(c, http.StatusInternalServerError, "account_create_failed", "could not create account")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	start := time.Now()
	err = h.client.Set(ctx, authKey(tenantID), security.HashAPIKey(apiKey), 0).Err()
	h.metrics.RecordRedisOperation("SET", "account_create", start, err)
	if err != nil {
		h.metrics.RecordAPIKeyCreation("failure", "redis_error")
		writeError(c, http.StatusInternalServerError, "account_create_failed", "could not create account")
		return
	}

	h.metrics.RecordAPIKeyCreation("success", "account_create")
	c.JSON(http.StatusCreated, gin.H{
		"tenant_id": tenantID,
		"api_key":   apiKey,
	})
}

func (h *AuthHandler) AuthenticateAPIKey(ctx context.Context, tenantID string, apiKey string) bool {
	if !security.ValidateTenantID(tenantID) || apiKey == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	start := time.Now()
	storedHash, err := h.client.Get(ctx, authKey(tenantID)).Result()
	h.metrics.RecordRedisOperation("GET", "auth", start, err)
	if err != nil {
		return false
	}

	return security.CompareAPIKeyHash(storedHash, apiKey)
}

func (h *AuthHandler) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	start := time.Now()
	err := h.client.Ping(ctx).Err()
	h.metrics.RecordRedisOperation("PING", "auth_health", start, err)
	return err
}

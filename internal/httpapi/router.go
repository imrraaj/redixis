package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"redixis/internal/config"
	"redixis/internal/observability"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type Dependencies struct {
	Config      config.Config
	Logger      *slog.Logger
	Metrics     observability.Recorder
	AuthRedis   *redis.Client
	TenantRedis *redis.Client
}

func NewRouter(deps Dependencies) *gin.Engine {
	if deps.Config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(deps.Logger))
	router.Use(deps.Metrics.HTTPMiddleware())

	router.GET("/docs", swaggerUIHandler)
	router.GET("/openapi.yaml", openAPIHandler)
	router.GET("/metrics", gin.WrapH(deps.Metrics.Handler()))
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authHandler := NewAuthHandler(deps.AuthRedis, deps.Config.RedisOperationTimeout, deps.Metrics)
	router.GET("/readyz", readinessHandler(authHandler, deps.TenantRedis, deps.Metrics))

	auth := router.Group("/auth")
	auth.POST("/account", authHandler.CreateAccount)

	tenantHandler := NewTenantHandler(deps.TenantRedis, deps.Config.RedisOperationTimeout, deps.Metrics)
	tenant := router.Group("/v1")
	tenant.Use(commandGuard())
	tenant.Use(apiKeyAuth(authHandler, deps.Metrics))
	tenant.Use(rateLimit(deps.AuthRedis, deps.Config.RedisOperationTimeout, deps.Metrics, deps.Config.RateLimitPerMinute))
	tenant.POST("/:tenant_id/:command", tenantHandler.HandleCommand)

	return router
}

func readinessHandler(authHandler *AuthHandler, tenantRedis *redis.Client, metrics observability.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := authHandler.Ping(ctx); err != nil {
			writeError(c, http.StatusServiceUnavailable, "auth_redis_unavailable", "auth redis is not ready")
			return
		}

		start := time.Now()
		err := tenantRedis.Ping(ctx).Err()
		metrics.RecordRedisOperation("PING", "tenant_health", start, err)
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, "tenant_redis_unavailable", "tenant redis is not ready")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

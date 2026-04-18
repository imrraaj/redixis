package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func rateLimitMiddleware(ctx context.Context, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Params.ByName("tenant")

		if validateTenantID(tenantID) {
			recordRateLimitDecision("rejected", "invalid_tenant")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rateKey := "rate:" + tenantID
		redisStart := time.Now()
		count, err := rdb.Incr(ctx, rateKey).Result()
		recordRedisOperation("INCR", "rate_limit", redisStart, err)
		if err != nil {
			recordRateLimitDecision("error", "redis_error")
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if count == 1 {
			redisStart = time.Now()
			err = rdb.Expire(ctx, rateKey, time.Minute).Err()
			recordRedisOperation("EXPIRE", "rate_limit", redisStart, err)
		}
		if count > 5 {
			recordRateLimitDecision("rejected", "limit_exceeded")
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		recordRateLimitDecision("allowed", "ok")
		c.Next()
	}
}

func APIAuthMiddleware(ctx context.Context, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Params.ByName("tenant")
		if len(tenantID) != 16 {
			recordAuthAttempt("failure", "invalid_tenant")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		apiKey := c.GetHeader("X-Authorization")
		if apiKey == "" {
			recordAuthAttempt("failure", "missing_api_key")
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		redisStart := time.Now()
		api, err := rdb.Get(ctx, tenantID).Result()
		recordRedisOperation("GET", "auth", redisStart, err)
		if err != nil {
			if err == redis.Nil {
				recordAuthAttempt("failure", "tenant_not_found")
			} else {
				recordAuthAttempt("failure", "redis_error")
			}
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		if api != apiKey {
			recordAuthAttempt("failure", "invalid_api_key")
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		recordAuthAttempt("success", "ok")
		c.Next()
	}
}

func main() {

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = os.Getenv("REDIS_URI")
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Username: os.Getenv("REDIS_USERNAME"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	defer rdb.Close()

	r := gin.Default()
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.Use(prometheusMiddleware())

	srv := http.Server{
		Addr:        "0.0.0.0:8080",
		Handler:     r,
		ReadTimeout: 5 * time.Second,
	}

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	r.GET("/apikey", func(c *gin.Context) {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			recordAPIKeyCreation("failure", "random_error")
			c.Status(http.StatusInternalServerError)
			return
		}
		key := hex.EncodeToString(b)
		fmt.Println(key[:16], key[16:])
		redisStart := time.Now()
		err := rdb.Set(ctx, key[:16], key[16:], 0).Err()
		recordRedisOperation("SET", "api_key_create", redisStart, err)
		if err != nil {
			recordAPIKeyCreation("failure", "redis_error")
			c.Status(http.StatusInternalServerError)
			return
		}
		recordAPIKeyCreation("success", "ok")
		c.String(http.StatusOK, key)
	})

	tenantInfra := r.Group("/v1")
	// tenantInfra.Use(rateLimitMiddleware(ctx, rdb))
	tenantInfra.Use(APIAuthMiddleware(ctx, rdb))

	tenantInfra.POST("/:tenant/GET", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("GET", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key
		redisStart := time.Now()
		val, err := rdb.Get(ctx, getKey).Result()
		recordRedisOperation("GET", "tenant_get", redisStart, err)
		if err != nil {
			if err == redis.Nil {
				recordTenantOperation("GET", "not_found", operationStart)
				c.Status(http.StatusNotFound)
				return
			}
			recordTenantOperation("GET", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("GET", "success", operationStart)
		c.String(http.StatusOK, val)
	})
	tenantInfra.POST("/:tenant/SET", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key   string      `json:"key"`
			Value interface{} `json:"value"`
			TTL   int         `json:"ttl" default:"0"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("SET", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}

		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key
		fmt.Println(getKey)
		redisStart := time.Now()
		err := rdb.Set(ctx, getKey, body.Value, time.Duration(body.TTL)*time.Second).Err()
		recordRedisOperation("SET", "tenant_set", redisStart, err)
		if err != nil {
			recordTenantOperation("SET", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("SET", "success", operationStart)
		c.Status(http.StatusOK)
	})

	tenantInfra.POST("/:tenant/DEL", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key []string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("DEL", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		redisKeys := make([]string, 0, len(body.Key))
		tenantID := c.Params.ByName("tenant")
		for _, key := range body.Key {
			redisKeys = append(redisKeys, "tenant:"+tenantID+":"+key)
		}

		redisStart := time.Now()
		val, err := rdb.Del(ctx, redisKeys...).Result()
		recordRedisOperation("DEL", "tenant_delete", redisStart, err)
		if err != nil {
			recordTenantOperation("DEL", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("DEL", "success", operationStart)
		c.JSON(http.StatusOK, val)
	})

	tenantInfra.POST("/:tenant/INCR", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("INCR", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key

		redisStart := time.Now()
		val, err := rdb.Incr(ctx, getKey).Result()
		recordRedisOperation("INCR", "tenant_incr", redisStart, err)
		if err != nil {
			recordTenantOperation("INCR", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("INCR", "success", operationStart)
		c.String(http.StatusOK, fmt.Sprintf("%d", val))
	})

	tenantInfra.POST("/:tenant/DECR", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("DECR", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key

		redisStart := time.Now()
		val, err := rdb.Decr(ctx, getKey).Result()
		recordRedisOperation("DECR", "tenant_decr", redisStart, err)
		if err != nil {
			recordTenantOperation("DECR", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("DECR", "success", operationStart)
		c.String(http.StatusOK, fmt.Sprintf("%d", val))
	})

	tenantInfra.POST("/:tenant/MGET", func(c *gin.Context) {
		operationStart := time.Now()
		var body struct {
			Key []string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("MGET", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		redisKeys := make([]string, 0, len(body.Key))
		tenantID := c.Params.ByName("tenant")
		for _, key := range body.Key {
			redisKeys = append(redisKeys, "tenant:"+tenantID+":"+key)
		}

		redisStart := time.Now()
		val, err := rdb.MGet(ctx, redisKeys...).Result()
		recordRedisOperation("MGET", "tenant_mget", redisStart, err)
		if err != nil {
			recordTenantOperation("MGET", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("MGET", "success", operationStart)
		c.JSON(http.StatusOK, val)
	})

	tenantInfra.POST("/:tenant/MSET", func(c *gin.Context) {
		operationStart := time.Now()
		var body map[string]string
		if err := c.BindJSON(&body); err != nil {
			recordTenantOperation("MSET", "bad_request", operationStart)
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		temp := map[string]interface{}{}
		for k, v := range body {
			temp["tenant:"+tenantID+":"+k] = v
		}

		redisStart := time.Now()
		val, err := rdb.MSet(ctx, temp).Result()
		recordRedisOperation("MSET", "tenant_mset", redisStart, err)
		if err != nil {
			recordTenantOperation("MSET", "redis_error", operationStart)
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		recordTenantOperation("MSET", "success", operationStart)
		c.String(http.StatusOK, val)
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
		log.Printf("Listening on %s", srv.Addr)
	}()

	<-ctx.Done()
	stop()

	log.Println("shutting down gracefully, press Ctrl+C again to force")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("Server forced to shutdown: ", err)
	}
	log.Println("Server shutdown")
}

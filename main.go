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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

var (
	HttpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint"},
	)
	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	HttpTotalRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_total_request_errors",
			Help: "Total number of HTTP request errors",
		},
		[]string{"method", "endpoint"},
	)

	CPUTotalUsage = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cpu_total_usage_seconds",
			Help:    "Total CPU usage in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tenant"},
	)

	MemoryTotalUsage = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "memory_total_usage_bytes",
			Help:    "Total memory usage in bytes",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tenant"},
	)
)

func rateLimitMiddleware(ctx context.Context, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Params.ByName("tenant")

		if validateTenantID(tenantID) {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rateKey := "rate:" + tenantID
		count, err := rdb.Incr(ctx, rateKey).Result()
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if count == 1 {
			rdb.Expire(ctx, rateKey, time.Minute)
		}
		if count > 5 {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

func APIAuthMiddleware(ctx context.Context, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Params.ByName("tenant")
		if len(tenantID) != 16 {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		apiKey := c.GetHeader("X-Authorization")
		if apiKey == "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		if api, err := rdb.Get(ctx, tenantID).Result(); err != nil || api != apiKey {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

func main() {

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	rdb := redis.NewClient(&redis.Options{
		// Addr:     os.Getenv("REDIS_URI"),
		Addr:     "localhost:6379",
		Username: os.Getenv("REDIS_USERNAME"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	defer rdb.Close()

	r := gin.Default()
	srv := http.Server{
		Addr:        ":8080",
		Handler:     r,
		ReadTimeout: 5 * time.Second,
	}

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	r.GET("/apikey", func(c *gin.Context) {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		key := hex.EncodeToString(b)
		fmt.Println(key[:16], key[16:])
		if err := rdb.Set(ctx, key[:16], key[16:], 0).Err(); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, key)
	})

	tenantInfra := r.Group("/v1")
	// tenantInfra.Use(rateLimitMiddleware(ctx, rdb))
	tenantInfra.Use(APIAuthMiddleware(ctx, rdb))

	tenantInfra.POST("/:tenant/GET", func(c *gin.Context) {
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key
		val, err := rdb.Get(ctx, getKey).Result()
		if err != nil {
			if err == redis.Nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.String(http.StatusOK, val)
	})
	tenantInfra.POST("/:tenant/SET", func(c *gin.Context) {
		var body struct {
			Key   string      `json:"key"`
			Value interface{} `json:"value"`
			TTL   int         `json:"ttl" default:"0"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}

		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key
		fmt.Println(getKey)
		err := rdb.Set(ctx, getKey, body.Value, time.Duration(body.TTL)*time.Second).Err()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.Status(http.StatusOK)
	})

	tenantInfra.POST("/:tenant/DEL", func(c *gin.Context) {
		var body struct {
			Key []string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		redisKeys := make([]string, 0, len(body.Key))
		tenantID := c.Params.ByName("tenant")
		for _, key := range body.Key {
			redisKeys = append(redisKeys, "tenant:"+tenantID+":"+key)
		}

		val, err := rdb.Del(ctx, redisKeys...).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.JSON(http.StatusOK, val)
	})

	tenantInfra.POST("/:tenant/INCR", func(c *gin.Context) {
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key

		val, err := rdb.Incr(ctx, getKey).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.String(http.StatusOK, string(val))
	})

	tenantInfra.POST("/:tenant/DECR", func(c *gin.Context) {
		var body struct {
			Key string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body.Key

		val, err := rdb.Decr(ctx, getKey).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.String(http.StatusOK, string(val))
	})

	tenantInfra.POST("/:tenant/MGET", func(c *gin.Context) {
		var body struct {
			Key []string `json:"key"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		redisKeys := make([]string, 0, len(body.Key))
		tenantID := c.Params.ByName("tenant")
		for _, key := range body.Key {
			redisKeys = append(redisKeys, "tenant:"+tenantID+":"+key)
		}

		val, err := rdb.MGet(ctx, redisKeys...).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
		c.JSON(http.StatusOK, val)
	})

	tenantInfra.POST("/:tenant/MSET", func(c *gin.Context) {
		var body map[string]string
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		temp := map[string]interface{}{}
		for k, v := range body {
			temp["tenant:"+tenantID+":"+k] = v
		}

		val, err := rdb.MSet(ctx, temp).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			log.Println(err)
			return
		}
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

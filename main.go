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
	"github.com/redis/go-redis/v9"
)

func APIAuthMiddleware(ctx context.Context, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Params.ByName("tenant")
		if len(tenantID) != 16 {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		apiKey := c.GetHeader("X-Authorization")
		if apiKey == "" {
			c.AbortWithStatus(http.StatusBadRequest)
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
	tenantInfra.Use(APIAuthMiddleware(ctx, rdb))
	tenantInfra.POST("/:tenant/GET", func(c *gin.Context) {
		var body map[string]string
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body["key"]
		val, err := rdb.Get(ctx, getKey).Result()
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.String(http.StatusOK, val)
	})
	tenantInfra.POST("/:tenant/SET", func(c *gin.Context) {
		var body map[string]string
		if err := c.BindJSON(&body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		tenantID := c.Params.ByName("tenant")
		getKey := "tenant:" + tenantID + body["key"]
		err := rdb.Set(ctx, getKey, body["value"], 0).Err()
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
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

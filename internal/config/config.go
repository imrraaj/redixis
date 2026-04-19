package config

import (
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment           string
	HTTPAddr              string
	ShutdownTimeout       time.Duration
	RedisOperationTimeout time.Duration
	RateLimitPerMinute    int64
	AuthRedisURL          string
	TenantRedisURL        string
}

func Load() Config {
	redisTimeout := durationFromEnv("REDIS_OPERATION_TIMEOUT", time.Second)

	return Config{
		Environment:           stringFromEnv("APP_ENV", "development"),
		HTTPAddr:              stringFromEnv("HTTP_ADDR", "0.0.0.0:8080"),
		ShutdownTimeout:       durationFromEnv("SHUTDOWN_TIMEOUT", 5*time.Second),
		RedisOperationTimeout: redisTimeout,
		RateLimitPerMinute:    int64FromEnv("RATE_LIMIT_PER_MINUTE", 120),
		AuthRedisURL:          stringFromEnv("AUTH_REDIS_URL", defaultRedisURL("localhost:6379", "", "auth-dev-pass", 0, 20, redisTimeout)),
		TenantRedisURL:        stringFromEnv("TENANT_REDIS_URL", defaultRedisURL("localhost:6380", "redixis", "tenant-dev-pass", 0, 50, redisTimeout)),
	}
}

func defaultRedisURL(addr string, username string, password string, db int, poolSize int, timeout time.Duration) string {
	u := &url.URL{
		Scheme: "redis",
		Host:   addr,
		Path:   "/" + strconv.Itoa(db),
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}

	query := u.Query()
	query.Set("pool_size", strconv.Itoa(poolSize))
	query.Set("dial_timeout", timeout.String())
	query.Set("read_timeout", timeout.String())
	query.Set("write_timeout", timeout.String())
	u.RawQuery = query.Encode()

	return u.String()
}

func stringFromEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func int64FromEnv(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err == nil {
		return parsed
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

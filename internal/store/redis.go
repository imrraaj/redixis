package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisObserver interface {
	RecordRedisOperation(operation string, purpose string, start time.Time, err error)
}

func NewRedisClient(redisURL string) *redis.Client {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		panic(fmt.Errorf("invalid redis URL %q: %w", redisURL, err))
	}

	return redis.NewClient(options)
}

func withTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

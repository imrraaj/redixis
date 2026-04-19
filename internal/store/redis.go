package store

import (
	"fmt"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(redisURL string) *redis.Client {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		panic(fmt.Errorf("invalid redis URL %q: %w", redisURL, err))
	}

	return redis.NewClient(options)
}

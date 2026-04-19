package bench

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newDirectClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     "localhost:6380",
		Username: "redixis",
		Password: "tenant-dev-pass",
		DB:       0,
	})
}

// BenchmarkDirectRedis_SET tests SET operations directly against Redis
func BenchmarkDirectRedis_SET(b *testing.B) {
	ctx := context.Background()
	client := newDirectClient()
	defer client.Close()

	tenantID := "benchdirect001"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("tenant:{%s}:key%d", tenantID, i)
			err := client.Set(ctx, key, "value", 60*time.Second).Err()
			if err != nil {
				b.Fatalf("set failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkDirectRedis_GET tests GET operations directly against Redis
func BenchmarkDirectRedis_GET(b *testing.B) {
	ctx := context.Background()
	client := newDirectClient()
	defer client.Close()

	tenantID := "benchdirect001"
	// Pre-populate keys
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("tenant:{%s}:key%d", tenantID, i)
		client.Set(ctx, key, "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("tenant:{%s}:key%d", tenantID, i%1000)
			_, err := client.Get(ctx, key).Result()
			if err != nil && err != redis.Nil {
				b.Fatalf("get failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkDirectRedis_INCR tests INCR operations directly against Redis
func BenchmarkDirectRedis_INCR(b *testing.B) {
	ctx := context.Background()
	client := newDirectClient()
	defer client.Close()

	tenantID := "benchdirect001"
	// Pre-populate counter
	client.Set(ctx, fmt.Sprintf("tenant:{%s}:counter", tenantID), 0, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Incr(ctx, fmt.Sprintf("tenant:{%s}:counter", tenantID)).Result()
		if err != nil {
			b.Fatalf("incr failed: %v", err)
		}
	}
}

// BenchmarkDirectRedis_MSET tests MSET operations directly against Redis
func BenchmarkDirectRedis_MSET(b *testing.B) {
	ctx := context.Background()
	client := newDirectClient()
	defer client.Close()

	tenantID := "benchdirect001"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pairs := make(map[string]interface{})
		for j := 0; j < 10; j++ {
			key := fmt.Sprintf("tenant:{%s}:batch%d_key%d", tenantID, i, j)
			pairs[key] = fmt.Sprintf("value%d", j)
		}
		err := client.MSet(ctx, pairs).Err()
		if err != nil {
			b.Fatalf("mset failed: %v", err)
		}
	}
}

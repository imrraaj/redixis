# Benchmark Suite

This directory contains benchmarks comparing direct Redis operations vs Redixis API overhead.

## Files

| File | Purpose |
|------|---------|
| `direct_redis_test.go` | Benchmarks hitting Redis directly (bypass Redixis) |
| `redixis_api_test.go` | Benchmarks hitting Redixis HTTP API |
| `k6_load_test.js` | k6 load test (ramp up to 500 VUs) |
| `k6_crash_test.js` | k6 crash test (push to 2000 VUs until failure) |

## Usage

### Go Benchmarks (Direct vs Redixis)

```bash
# Start infrastructure (Redis + Redixis)
make infra-up

# In another terminal, run Redixis API
make run

# Run direct Redis benchmarks (no auth)
make bench-direct

# Run Redixis API benchmarks (requires server on :8080)
make bench-redixis

# Run both and compare
make bench-compare
```

### k6 Load Tests

```bash
# Prerequisites: Install k6
# https://k6.io/docs/get-started/installation/

# Start Redixis server
make run

# Run load test (up to 500 virtual users)
make bench-k6-load

# Run crash test (up to 2000 VUs, find breaking point)
make bench-k6-crash
```

## Expected Overhead

| Operation | Direct Redis | Redixis API | Expected Overhead |
|-----------|-------------|-------------|-------------------|
| SET | ~0.5ms | ~2-5ms | 4-10x (HTTP + auth + validation) |
| GET | ~0.3ms | ~2-4ms | 6-13x |
| INCR | ~0.4ms | ~2-4ms | 5-10x |
| MSET (10 keys) | ~0.5ms | ~3-6ms | 6-12x |

Redixis adds:
- HTTP request/response parsing
- JSON marshal/unmarshal
- API key authentication (Redis lookup)
- Rate limiting (Redis INCR)
- Tenant ID validation
- Key prefixing (`tenant:{id}:key`)

## Sample Results

```
BenchmarkDirectRedis_SET-8        10000    520 ns/op
BenchmarkRedixis_SET-8            2000    2500 ns/op
# Overhead: ~4.8x
```

The overhead is expected — Redixis provides multi-tenancy, auth, rate limiting, and metrics at the cost of latency.

# Redixis

Redixis is a multi-tenant Redis gateway with API-key auth, command allow-listing, Prometheus metrics, and Grafana dashboards.

## Architecture

- `redis-auth`: persistent Redis with AOF enabled. Stores accounts, tenant metadata, API-key hashes, and rate-limit buckets.
- `redis-tenant`: isolated tenant data Redis. Uses Redis ACLs and only allows the gateway user to run the supported command set against `tenant:*` keys.
- `redixis-api`: Go API service.
- `prometheus`: scrapes `/metrics`.
- `grafana`: provisions Prometheus and the Redixis dashboard.

## Run

```bash
docker compose up --build
```

Or use the Makefile:

```bash
make compose-up
```

Run the Go API locally while keeping Redis, Prometheus, and Grafana in containers:

```bash
docker compose up -d auth_redis tenant_redis prometheus grafana
go run ./cmd/redixis
```

Or use the Makefile:

```bash
make run
```

Prometheus is configured to scrape `host.docker.internal:8080`, so the same setup works when the API runs on the host or when it runs in Compose and publishes port `8080`.

Useful URLs:

- API: `http://localhost:8080`
- API docs: `http://localhost:8080/docs`
- OpenAPI spec: `http://localhost:8080/openapi.yaml`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`
- Grafana login: `admin` / `password`

Override the Redis URLs when needed:

```bash
AUTH_REDIS_URL='redis://:replace-me@auth_redis:6379/0?pool_size=20&dial_timeout=1s&read_timeout=1s&write_timeout=1s' \
TENANT_REDIS_URL='redis://redixis:replace-me@tenant_redis:6379/0?pool_size=50&dial_timeout=1s&read_timeout=1s&write_timeout=1s' \
RATE_LIMIT_PER_MINUTE=1200 \
docker compose up --build -d
```

If you run the API directly on the host, the built-in defaults already point at `localhost:6379` and `localhost:6380`.

## API

Create a demo account and tenant:

```bash
curl -s http://localhost:8080/auth/account \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo"}'
```

Run tenant commands:

```bash
curl -s http://localhost:8080/v1/$TENANT_ID/SET \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello","value":"world","ttl_seconds":60}'

curl -s http://localhost:8080/v1/$TENANT_ID/GET \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello"}'
```

Supported commands:

- `GET`: `{"key":"name"}`
- `SET`: `{"key":"name","value":"value","ttl_seconds":60}`
- `DEL`: `{"keys":["a","b"]}`
- `INCR`: `{"key":"counter"}`
- `DECR`: `{"key":"counter"}`
- `MGET`: `{"keys":["a","b"]}`
- `MSET`: `{"items":{"a":"1","b":"2"}}`

## Health And Metrics

- `GET /healthz`: process liveness.
- `GET /readyz`: checks both Redis instances.
- `GET /metrics`: Prometheus metrics.

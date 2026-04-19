APP_NAME := redixis
APP_BIN := ./bin/$(APP_NAME)
COMPOSE := docker compose
INFRA_SERVICES := auth_redis tenant_redis prometheus grafana

.PHONY: help infra-up infra-down infra-logs infra-ps run build compose-build compose-up compose-down compose-logs fmt vet test check clean bench bench-direct bench-redixis bench-compare bench-k6-load bench-k6-crash

help:
	@printf '%s\n' \
		'make infra-up       Start Redis, Prometheus, and Grafana' \
		'make infra-down     Stop infra containers' \
		'make infra-logs     Tail infra logs' \
		'make infra-ps       Show infra container status' \
		'make run            Run the Go API against local infra' \
		'make build          Build the API binary to ./bin/redixis' \
		'make compose-build  Build the API image' \
		'make compose-up     Start the full stack in Compose' \
		'make compose-down   Stop the full stack' \
		'make compose-logs   Tail full stack logs' \
		'make fmt            Format Go code' \
		'make vet            Run go vet' \
		'make test           Run go test' \
		'make check          Run fmt, vet, test, and build' \
		'make bench          Run all benchmarks (direct vs redixis)' \
		'make bench-direct   Benchmark direct Redis operations' \
		'make bench-redixis  Benchmark Redixis API operations' \
		'make bench-compare  Compare both benchmarks' \
		'make bench-k6-load  Run k6 load test (requires k6)' \
		'make bench-k6-crash Run k6 crash test (requires k6)' \
		'make clean          Remove local build artifacts'

infra-up:
	$(COMPOSE) up -d $(INFRA_SERVICES)

infra-down:
	$(COMPOSE) stop $(INFRA_SERVICES)

infra-logs:
	$(COMPOSE) logs -f $(INFRA_SERVICES)

infra-ps:
	$(COMPOSE) ps $(INFRA_SERVICES)

run: infra-up
	go run ./cmd/redixis

build:
	mkdir -p ./bin
	go build -o $(APP_BIN) ./cmd/redixis

compose-build:
	$(COMPOSE) build redixis_api

compose-up:
	$(COMPOSE) up --build

compose-down:
	$(COMPOSE) down

compose-logs:
	$(COMPOSE) logs -f

fmt:
	gofmt -w $$(find cmd internal -name '*.go')

vet:
	go vet ./...

test:
	go test ./...

check: fmt vet test build

clean:
	rm -rf ./bin

bench-direct:
	@echo "Running direct Redis benchmarks..."
	@go test -bench=BenchmarkDirectRedis -benchmem ./bench/...

bench-redixis:
	@echo "Running Redixis API benchmarks..."
	@go test -bench=BenchmarkRedixis -benchmem ./bench/...

bench: bench-direct bench-redixis

bench-compare:
	@echo "=== Direct Redis (baseline) ==="
	@go test -bench=BenchmarkDirectRedis -benchmem -count=3 ./bench/...
	@echo ""
	@echo "=== Redixis API (with overhead) ==="
	@go test -bench=BenchmarkRedixis -benchmem -count=3 ./bench/...

bench-k6-load:
	@echo "Running k6 load test (requires k6 installed)..."
	@k6 run --vus 100 --duration 30s ./bench/k6_load_test.js

bench-k6-crash:
	@echo "Running k6 crash test (requires k6 installed)..."
	@k6 run ./bench/k6_crash_test.js

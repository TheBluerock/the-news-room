.PHONY: dev-up dev-down vault-seed migrate-up migrate-down \
        redpanda-setup dlq-list dlq-replay load-test proto \
        test test-go test-python lint lint-go lint-python \
        build-all swarm-deploy swarm-status swarm-rollback

ENV    ?= local
TAG    ?= dev
STACK  ?= newsroom

DB_URL_local   := postgres://newsroom:newsroom_dev@localhost:5432/newsroom?sslmode=disable
DB_URL_staging := $(STAGING_DB_URL)
DB_URL_prod    := $(PROD_DB_URL)
DB_URL         := $(DB_URL_$(ENV))

VAULT_ADDR_local   := http://localhost:8200
VAULT_ADDR_staging := $(STAGING_VAULT_ADDR)
VAULT_ADDR         := $(VAULT_ADDR_$(ENV))

GO_SERVICES := auth learner analytics sanity
PY_SERVICES := agent moderation

INFRA_SERVICES := postgres redis redpanda vault tempo prometheus grafana

dev-up:
	@echo "==> Starting infrastructure..."
	docker compose -f docker-compose.dev.yml up -d --wait $(INFRA_SERVICES)
	@echo "==> Seeding Vault..."
	$(MAKE) vault-seed
	@echo "==> Running migrations..."
	$(MAKE) migrate-up ENV=local
	@echo "==> Setting up Redpanda topics and schemas..."
	$(MAKE) redpanda-setup
	@echo "==> Starting services..."
	docker compose -f docker-compose.dev.yml up -d --wait

dev-down:
	docker compose -f docker-compose.dev.yml down -v

vault-seed:
	VAULT_ADDR=$(VAULT_ADDR_local) VAULT_TOKEN=dev-root-token \
		bash infra/vault/scripts/seed.sh

MIGRATE := go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest

migrate-up:
	@test -n "$(DB_URL)" || (echo "ERROR: unknown ENV=$(ENV)"; exit 1)
	$(MIGRATE) -path infra/migrations/postgres \
	           -database "$(DB_URL)" up

migrate-down:
	@test -n "$(DB_URL)" || (echo "ERROR: unknown ENV=$(ENV)"; exit 1)
	$(MIGRATE) -path infra/migrations/postgres \
	           -database "$(DB_URL)" down 1

redpanda-setup:
	chmod +x infra/scripts/setup-redpanda.sh
	bash infra/scripts/setup-redpanda.sh

dlq-list:
	go run ./cmd/dlq-tool list

dlq-replay:
	@test -n "$(TOPIC)" || (echo "ERROR: TOPIC is required"; exit 1)
	go run ./cmd/dlq-tool replay --topic $(TOPIC)

dlq-discard:
	@test -n "$(TOPIC)" || (echo "ERROR: TOPIC is required"; exit 1)
	go run ./cmd/dlq-tool discard --topic $(TOPIC)

load-test:
	k6 run infra/load-test/k6-script.js

# ── Build ──────────────────────────────────────────────────────────────────────

build-all:
	docker build -t newsroom/auth:$(TAG)            services/auth
	docker build -t newsroom/learner-server:$(TAG)  -f services/learner/Dockerfile.server services/learner
	docker build -t newsroom/learner-ingest:$(TAG)  -f services/learner/Dockerfile.ingest services/learner
	docker build -t newsroom/agent:$(TAG)           services/agent
	docker build -t newsroom/moderation:$(TAG)      services/moderation
	docker build -t newsroom/analytics:$(TAG)       services/analytics
	docker build -t newsroom/sanity:$(TAG)          services/sanity

# ── Swarm ──────────────────────────────────────────────────────────────────────

SWARM_FILE_dev  := infra/swarm/stack.dev.yml
SWARM_FILE_prod := infra/swarm/stack.prod.yml
SWARM_FILE      := $(SWARM_FILE_$(ENV))

swarm-deploy:
	@test -n "$(SWARM_FILE)" || (echo "ERROR: unknown ENV=$(ENV)"; exit 1)
	docker stack deploy -c $(SWARM_FILE) --with-registry-auth $(STACK)

swarm-status:
	docker stack services $(STACK)

swarm-rollback:
	@test -n "$(SERVICE)" || (echo "ERROR: SERVICE is required. Usage: make swarm-rollback SERVICE=auth"; exit 1)
	docker service rollback $(STACK)_$(SERVICE)

proto:
	buf generate

test-go:
	@for svc in $(GO_SERVICES); do \
		echo "==> testing services/$$svc"; \
		(cd services/$$svc && go test -race -coverprofile=coverage.out ./... \
			&& go tool cover -func=coverage.out | tail -1); \
	done
	(cd cmd/dlq-tool && go test -race ./...)

test-python:
	(cd services/agent && pytest --cov=. --cov-fail-under=80 -q)
	(cd services/moderation && pytest --cov=. --cov-fail-under=80 -q)

test: test-go test-python

lint-go:
	@for svc in $(GO_SERVICES); do \
		echo "==> linting services/$$svc"; \
		(cd services/$$svc && golangci-lint run ./...); \
	done

lint-python:
	(cd services/agent && ruff check . && mypy .)
	(cd services/moderation && ruff check . && mypy .)

lint: lint-go lint-python

# Proto buf check
buf-lint:
	buf lint
	buf breaking --against '.git#branch=main'

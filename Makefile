.PHONY: dev-up dev-down vault-seed migrate-up migrate-down \
        dlq-list dlq-replay load-test proto \
        test test-go test-python lint lint-go lint-python

ENV ?= local

DB_URL_local   := postgres://newsroom:newsroom_dev@localhost:5432/newsroom?sslmode=disable
DB_URL_staging := $(STAGING_DB_URL)
DB_URL_prod    := $(PROD_DB_URL)
DB_URL         := $(DB_URL_$(ENV))

VAULT_ADDR_local   := http://localhost:8200
VAULT_ADDR_staging := $(STAGING_VAULT_ADDR)
VAULT_ADDR         := $(VAULT_ADDR_$(ENV))

GO_SERVICES := auth learner correction analytics
PY_SERVICES := agent moderation

dev-up:
	docker compose -f docker-compose.dev.yml up -d --wait
	@echo "Stack is up. Seeding Vault and running migrations..."
	$(MAKE) vault-seed
	$(MAKE) migrate-up ENV=local

dev-down:
	docker compose -f docker-compose.dev.yml down -v

vault-seed:
	VAULT_ADDR=$(VAULT_ADDR_local) VAULT_TOKEN=dev-root-token \
		bash infra/vault/scripts/seed.sh

migrate-up:
	@test -n "$(DB_URL)" || (echo "ERROR: unknown ENV=$(ENV)"; exit 1)
	migrate -path infra/migrations/postgres \
	        -database "$(DB_URL)" up

migrate-down:
	@test -n "$(DB_URL)" || (echo "ERROR: unknown ENV=$(ENV)"; exit 1)
	migrate -path infra/migrations/postgres \
	        -database "$(DB_URL)" down 1

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

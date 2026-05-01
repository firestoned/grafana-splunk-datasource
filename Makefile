PLUGIN_NAME  := firestoned-splunk-datasource
DOCKER_IMAGE := $(PLUGIN_NAME)
DOCKER_TAG   ?= latest
MAGE_VERSION := v1.15.0
GOBIN        := $(shell go env GOPATH)/bin
MAGE         := $(shell command -v mage 2>/dev/null)

.PHONY: build build-frontend build-backend test test-frontend test-backend ensure-mage go-deps npm-deps dev dev-up dev-down dev-logs dev-restart

build: build-frontend build-backend

npm-deps: node_modules

node_modules: package.json package-lock.json
	npm ci
	@touch node_modules

build-frontend: npm-deps
	npm run build

go-deps:
	go mod tidy
	go mod download

build-backend: ensure-mage go-deps
	mage -v buildAll

ensure-mage:
ifndef MAGE
	@echo ">> mage not found, installing github.com/magefile/mage@$(MAGE_VERSION)"
	go install github.com/magefile/mage@$(MAGE_VERSION)
	@command -v mage >/dev/null || { \
	  echo "ERROR: mage installed to $(GOBIN) but not on PATH. Add it with: export PATH=\"$(GOBIN):$$PATH\""; \
	  exit 1; \
	}
endif

test: test-frontend test-backend

test-frontend: npm-deps
	npm run test:ci

test-backend:
	go test -race -coverprofile=coverage.out ./pkg/plugin/...

# ─────────────────────────────────────────────────────────────────────────────
# Local dev — run a Grafana OSS container with the plugin mounted from ./dist
# (see docker-compose.yaml). Backend binaries are loaded once at Grafana
# startup, so changes to Go code need `dev-restart`; frontend changes only
# need a `make build-frontend` followed by a browser refresh.
# ─────────────────────────────────────────────────────────────────────────────

dev: build dev-up

dev-up:
	docker compose up -d
	@echo ""
	@echo ">> Grafana is running at http://localhost:3000 (admin/admin)"
	@echo ">> 'make dev-logs' to follow logs · 'make dev-down' to stop"

dev-down:
	docker compose down

dev-logs:
	docker compose logs -f grafana

dev-restart: build
	docker compose restart grafana
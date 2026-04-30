PLUGIN_NAME  := firestoned-splunk-datasource
DOCKER_IMAGE := $(PLUGIN_NAME)
DOCKER_TAG   ?= latest
MAGE_VERSION := v1.15.0
GOBIN        := $(shell go env GOPATH)/bin
MAGE         := $(shell command -v mage 2>/dev/null)

.PHONY: build build-frontend build-backend test test-frontend test-backend ensure-mage go-deps npm-deps

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
	go test -race -coverprofile=coverage.out ./pkg/...
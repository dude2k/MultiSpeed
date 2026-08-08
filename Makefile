.DEFAULT_GOAL := help

.PHONY: help fmt vet lint test build web-install web-check web-test web-build api-lint e2e check docker-build docker-smoke clean

help:
	@echo "MultiSpeed targets: fmt vet lint test build web-check e2e check docker-build docker-smoke"

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

build: web-build
	go build ./cmd/multispeed

web-install:
	cd web && npm ci

web-check: web-install
	cd web && npm run lint
	cd web && npm run typecheck

web-test: web-install
	cd web && npm run test

web-build: web-install
	cd web && npm run build

api-lint:
	npx --yes @redocly/cli@2.41.1 lint openapi.yaml --format stylish

e2e: web-install
	cd web && npm run test:e2e

check: fmt vet lint test web-check web-test web-build api-lint

docker-build:
	docker build --platform linux/amd64 --tag multispeed:local .

docker-smoke:
	bash scripts/docker-smoke.sh multispeed:local

clean:
	go clean
	cd web && npm run clean --if-present

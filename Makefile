.PHONY: swagger swagger-check backend-format-check backend-vet backend-architecture backend-unit-test backend-test frontend-lint frontend-test frontend-build pre-push-check check-go-version install-hooks db-migrate-create db-migrate-up db-migrate-down-one db-migrate-status cli-build cli-snapshot cli-validate-release-matrix

CLI_VERSION ?= dev
CLI_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
CLI_BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
CLI_LDFLAGS := -X github.com/radiation/coyote-ci/backend/internal/versioninfo.Version=$(CLI_VERSION) -X github.com/radiation/coyote-ci/backend/internal/versioninfo.Commit=$(CLI_COMMIT) -X github.com/radiation/coyote-ci/backend/internal/versioninfo.BuildDate=$(CLI_BUILD_DATE)

GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.24.1
MIGRATIONS_DIR := backend/db/migrations
MIGRATE_DSN ?= postgres://coyote:coyote@localhost:5432/coyote_ci?sslmode=disable

swagger:
	cd backend && if command -v swag >/dev/null 2>&1; then \
		swag init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	else \
		go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	fi

swagger-check: swagger
	git diff --exit-code backend/docs

backend-format-check:
	@cd backend && fmt_out="$$(gofmt -l .)" && \
	if [ -n "$$fmt_out" ]; then \
		echo "These files need formatting:"; \
		echo "$$fmt_out"; \
		exit 1; \
	fi

backend-vet:
	cd backend && go vet ./...

backend-architecture:
	cd backend && go test ./architecture

backend-unit-test:
	@cd backend && packages="$$(go list ./... | grep -v '/architecture$$' || true)" && \
	if [ -z "$$packages" ]; then \
		echo "No non-architecture Go packages found"; \
		exit 1; \
	fi && \
	go test $$packages

backend-test: backend-architecture backend-unit-test

cli-build:
	cd backend && go build -ldflags "$(CLI_LDFLAGS)" -o ./tmp/coyote ./cmd/coyote

cli-snapshot:
	go run github.com/goreleaser/goreleaser/v2@v2.12.7 release --config .goreleaser.yml --snapshot --clean --skip=publish

cli-validate-release-matrix:
	bash ./scripts/validate_cli_snapshot.sh .

frontend-lint:
	cd frontend && npm run lint

frontend-test:
	cd frontend && npm run test:run

frontend-build:
	cd frontend && npm run build

pre-push-check: backend-test frontend-lint frontend-test frontend-build swagger-check

check-go-version:
	@echo "Checking Go version consistency (source of truth: backend/go.mod)..."
	@set -e; \
	go_version=$$(grep '^GO_VERSION=' .env | cut -d= -f2); \
	go_major_minor=$$(echo $$go_version | awk -F. '{print $$1"."$$2}'); \
	mod_go=$$(awk '/^go / {print $$2; exit}' backend/go.mod); \
	mod_toolchain=$$(awk '/^toolchain / {print $$2; exit}' backend/go.mod); \
	pipeline_image=$$(awk '/^  image: golang:/ {sub("  image: golang:", "", $$0); print $$0; exit}' .coyote/pipeline.yml); \
	if [ -z "$$go_version" ]; then echo "ERROR: GO_VERSION is missing in .env" >&2; exit 1; fi; \
	if [ "$$mod_go" != "$$go_major_minor" ]; then echo "ERROR: backend/go.mod go version ($$mod_go) does not match .env major.minor ($$go_major_minor)" >&2; exit 1; fi; \
	if [ "$$mod_toolchain" != "go$$go_version" ]; then echo "ERROR: backend/go.mod toolchain ($$mod_toolchain) does not match .env GO_VERSION ($$go_version)" >&2; exit 1; fi; \
	if [ "$$pipeline_image" != "$$go_version" ]; then echo "ERROR: .coyote/pipeline.yml golang image ($$pipeline_image) does not match .env GO_VERSION ($$go_version)" >&2; exit 1; fi; \
	dockerfile_default=$$(grep '^ARG GO_VERSION=' backend/Dockerfile | head -1 | cut -d= -f2); \
	if [ -z "$$dockerfile_default" ]; then echo "ERROR: backend/Dockerfile must have ARG GO_VERSION=<version>" >&2; exit 1; fi; \
	if [ "$$dockerfile_default" != "$$go_version" ]; then echo "ERROR: backend/Dockerfile default ($$dockerfile_default) does not match .env GO_VERSION ($$go_version)" >&2; exit 1; fi; \
	if ! grep -q 'GO_VERSION: $${GO_VERSION}' docker-compose.yml; then echo "ERROR: docker-compose.yml must pass GO_VERSION build args" >&2; exit 1; fi; \
	echo "Go version consistency check passed (GO_VERSION=$$go_version)"

install-hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks installed (.githooks)"

db-migrate-create:
	@if [ -z "$(name)" ]; then echo "Usage: make db-migrate-create name=<migration_name>"; exit 1; fi
	$(GOOSE) -dir $(MIGRATIONS_DIR) create $(name) sql

db-migrate-up:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(MIGRATE_DSN)" up

db-migrate-down-one:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(MIGRATE_DSN)" down-by-one

db-migrate-status:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(MIGRATE_DSN)" status
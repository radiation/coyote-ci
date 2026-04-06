.PHONY: swagger check-go-version install-hooks

swagger:
	cd backend && if command -v swag >/dev/null 2>&1; then \
		swag init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	else \
		go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	fi

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
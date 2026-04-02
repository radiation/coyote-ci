.PHONY: swagger check-go-version

swagger:
	cd backend && if command -v swag >/dev/null 2>&1; then \
		swag init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	else \
		go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	fi

check-go-version:
	@go_version=$$(grep '^GO_VERSION=' .env | cut -d= -f2); \
	go_major_minor=$$(echo $$go_version | awk -F. '{print $$1"."$$2}'); \
	mod_go=$$(awk '/^go / {print $$2; exit}' backend/go.mod); \
	mod_toolchain=$$(awk '/^toolchain / {print $$2; exit}' backend/go.mod); \
	pipeline_image=$$(awk '/^  image: golang:/ {sub("  image: golang:", "", $$0); print $$0; exit}' .coyote/pipeline.yml); \
	if [ -z "$$go_version" ]; then echo "GO_VERSION is missing in .env"; exit 1; fi; \
	if [ "$$mod_go" != "$$go_major_minor" ]; then echo "backend/go.mod go version ($$mod_go) does not match .env major.minor ($$go_major_minor)"; exit 1; fi; \
	if [ "$$mod_toolchain" != "go$$go_version" ]; then echo "backend/go.mod toolchain ($$mod_toolchain) does not match .env GO_VERSION ($$go_version)"; exit 1; fi; \
	if [ "$$pipeline_image" != "$$go_version" ]; then echo ".coyote/pipeline.yml golang image ($$pipeline_image) does not match .env GO_VERSION ($$go_version)"; exit 1; fi; \
	grep -q '^ARG GO_VERSION$$' backend/Dockerfile || (echo "backend/Dockerfile must use ARG GO_VERSION (no pinned fallback)" && exit 1); \
	grep -q 'GO_VERSION: $${GO_VERSION}' docker-compose.yml || (echo "docker-compose.yml must pass GO_VERSION build args" && exit 1); \
	echo "Go version consistency check passed (GO_VERSION=$$go_version)"
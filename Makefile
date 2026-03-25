.PHONY: swagger

swagger:
	cd backend && if command -v swag >/dev/null 2>&1; then \
		swag init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	else \
		go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --parseInternal -g ./cmd/server/main.go -o ./docs; \
	fi
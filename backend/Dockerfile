ARG GO_VERSION=1.27.1

FROM debian:bookworm-slim AS runtime-base

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
  && rm -rf /var/lib/apt/lists/*

FROM runtime-base AS artifact-runtime

COPY dist/coyote-server /app/server
COPY dist/coyote-worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

FROM golang:${GO_VERSION} AS base

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

FROM base AS source-builder

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker

FROM base AS migration-builder

RUN go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM runtime-base AS migrate-runtime

COPY --from=migration-builder /go/bin/goose /usr/local/bin/goose
COPY db/migrations /migrations

ENTRYPOINT ["/bin/sh", "-ec"]
CMD ["if [ -n \"${DATABASE_URL_FILE:-}\" ]; then if [ ! -r \"$DATABASE_URL_FILE\" ]; then echo \"DATABASE_URL_FILE is not readable: $DATABASE_URL_FILE\" >&2; exit 1; fi; DATABASE_URL=\"$(cat \"$DATABASE_URL_FILE\")\"; fi; if [ -z \"${DATABASE_URL:-}\" ]; then echo \"DATABASE_URL or DATABASE_URL_FILE is required\" >&2; exit 1; fi; exec goose -dir /migrations postgres \"$DATABASE_URL\" up"]

FROM base AS dev

RUN go install github.com/air-verse/air@v1.63.0

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

FROM runtime-base AS source-runtime

COPY --from=source-builder /app/server /app/server
COPY --from=source-builder /app/worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

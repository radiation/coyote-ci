ARG GO_VERSION=1.27.1

FROM golang:${GO_VERSION} AS base

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

FROM base AS source-builder

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker

FROM debian:bookworm-slim AS runtime-base

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

FROM runtime-base AS artifact-runtime

COPY dist/coyote-server /app/server
COPY dist/coyote-worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

FROM runtime-base

COPY --from=source-builder /app/server /app/server
COPY --from=source-builder /app/worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

FROM base AS dev

RUN go install github.com/air-verse/air@v1.63.0

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

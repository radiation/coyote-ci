ARG GO_VERSION=1.27.4

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

FROM base AS source

COPY . .

FROM source AS source-builder

ARG COYOTE_APPLICATION_VERSION=dev
ARG COYOTE_SERVER_VERSION=
ARG COYOTE_WORKER_VERSION=
ARG COYOTE_COMMIT=unknown
ARG COYOTE_BUILD_DATE=
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/radiation/coyote-ci/backend/internal/versioninfo.Version=${COYOTE_SERVER_VERSION:-$COYOTE_APPLICATION_VERSION} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.Commit=${COYOTE_COMMIT} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.BuildDate=${COYOTE_BUILD_DATE}" -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/radiation/coyote-ci/backend/internal/versioninfo.Version=${COYOTE_WORKER_VERSION:-$COYOTE_APPLICATION_VERSION} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.Commit=${COYOTE_COMMIT} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.BuildDate=${COYOTE_BUILD_DATE}" -o worker ./cmd/worker

FROM source AS mcp-builder

ARG COYOTE_APPLICATION_VERSION=dev
ARG COYOTE_COMMIT=unknown
ARG COYOTE_BUILD_DATE=
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X github.com/radiation/coyote-ci/backend/internal/versioninfo.Version=${COYOTE_APPLICATION_VERSION} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.Commit=${COYOTE_COMMIT} -X github.com/radiation/coyote-ci/backend/internal/versioninfo.BuildDate=${COYOTE_BUILD_DATE}" -o mcp ./cmd/mcp

FROM source AS migration-builder

RUN CGO_ENABLED=0 GOOS=linux go build -o migrate ./cmd/migrate

FROM runtime-base AS migrate-runtime

COPY --from=migration-builder /app/migrate /usr/local/bin/migrate
COPY db/migrations /migrations

ENTRYPOINT ["/usr/local/bin/migrate"]
CMD ["up"]

FROM base AS dev

RUN go install github.com/air-verse/air@v1.63.0

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

FROM runtime-base AS source-runtime-mcp

RUN apt-get update \
  && apt-get install -y --no-install-recommends curl \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --gid 10001 --system coyote \
  && useradd --uid 10001 --gid coyote --home-dir /app --shell /usr/sbin/nologin coyote

COPY --from=mcp-builder --chown=coyote:coyote /app/mcp /app/mcp

USER 10001:10001

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 CMD ["curl", "--fail", "--silent", "http://127.0.0.1:8081/readyz"]

ENTRYPOINT ["/app/mcp"]

FROM runtime-base AS source-runtime

COPY --from=source-builder /app/server /app/server
COPY --from=source-builder /app/worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

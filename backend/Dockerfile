FROM golang:1.26 AS dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go install github.com/air-verse/air@v1.63.0

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker

FROM debian:bookworm-slim

WORKDIR /app
COPY --from=builder /app/server /app/server
COPY --from=builder /app/worker /app/worker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

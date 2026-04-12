#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="/opt/coyote-ci/.env.prod"

echo "🔧 Starting Cloud SQL proxy..."
docker compose --env-file "$ENV_FILE" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  up -d cloudsql-proxy

echo "⏳ Waiting for proxy to be ready..."
sleep 5

echo "🗄️ Running migrations..."
docker compose --env-file "$ENV_FILE" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  run --rm migrate

echo "🚀 Starting application services..."
docker compose --env-file "$ENV_FILE" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  up -d --build server worker frontend

echo "📊 Status:"
docker compose --env-file "$ENV_FILE" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  ps
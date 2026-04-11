#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="/opt/coyote-ci/.env.prod"

docker compose --env-file "$ENV_FILE" \
  -f docker-compose.yml \
  -f docker-compose.prod.yml \
  logs -f
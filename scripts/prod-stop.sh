#!/usr/bin/env bash
set -eu pipefail

ENV_FILE="/opt/coyote-ci/.env.prod"

docker compose --env-file "$ENV_FILE" \
  -f docker-compose.prod.yml \
  down
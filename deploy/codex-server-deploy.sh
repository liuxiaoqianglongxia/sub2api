#!/usr/bin/env bash
set -euo pipefail

# Codex deployment helper for Sub2API.
# Safe defaults:
# - Uses /opt/sub2api
# - Binds to 127.0.0.1:9203 for reverse-proxy deployment
# - Generates secrets into .env without printing them
# - Does not overwrite an existing .env

DEPLOY_DIR="${DEPLOY_DIR:-/opt/sub2api}"
BIND_HOST="${BIND_HOST:-127.0.0.1}"
SERVER_PORT="${SERVER_PORT:-9203}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@example.com}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.local.yml}"
COMPOSE_URL="${COMPOSE_URL:-https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-compose.local.yml}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

rand_hex() {
  openssl rand -hex 32
}

need_cmd docker
need_cmd openssl
need_cmd curl

if ! docker compose version >/dev/null 2>&1; then
  echo "Missing required command: docker compose" >&2
  exit 1
fi

if [ ! -d "$DEPLOY_DIR" ]; then
  sudo mkdir -p "$DEPLOY_DIR"
  sudo chown "$USER":"$USER" "$DEPLOY_DIR"
fi

cd "$DEPLOY_DIR"

if [ ! -f "$COMPOSE_FILE" ]; then
  curl -fsSL "$COMPOSE_URL" -o "$COMPOSE_FILE"
fi

mkdir -p data postgres_data redis_data

if [ ! -f .env ]; then
  umask 077
  cat > .env <<EOF_ENV
BIND_HOST=${BIND_HOST}
SERVER_PORT=${SERVER_PORT}
SERVER_MODE=release
RUN_MODE=standard
TZ=Asia/Shanghai

POSTGRES_USER=sub2api
POSTGRES_PASSWORD=$(rand_hex)
POSTGRES_DB=sub2api

REDIS_PASSWORD=

ADMIN_EMAIL=${ADMIN_EMAIL}
ADMIN_PASSWORD=$(rand_hex)

JWT_SECRET=$(rand_hex)
JWT_EXPIRE_HOUR=24
TOTP_ENCRYPTION_KEY=$(rand_hex)

UPDATE_PROXY_URL=
EOF_ENV
  echo "Created .env with generated secrets at ${DEPLOY_DIR}/.env"
else
  echo "Using existing ${DEPLOY_DIR}/.env"
fi

docker compose -f "$COMPOSE_FILE" pull
docker compose -f "$COMPOSE_FILE" up -d
docker compose -f "$COMPOSE_FILE" ps

echo "Waiting for Sub2API health endpoint..."
for _ in $(seq 1 30); do
  if curl -fsS "http://${BIND_HOST}:${SERVER_PORT}/health" >/dev/null 2>&1; then
    echo "Sub2API is healthy at http://${BIND_HOST}:${SERVER_PORT}"
    exit 0
  fi
  sleep 2
done

echo "Sub2API did not become healthy within 60 seconds. Recent logs:" >&2
docker compose -f "$COMPOSE_FILE" logs --tail=100 sub2api >&2
exit 1

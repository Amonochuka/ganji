#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POLAR_COMPOSE="${POLAR_COMPOSE:-$HOME/.polar/networks/3/docker-compose.yml}"
LNBITS_COMPOSE="$ROOT_DIR/lnbits-docker-compose.yml"
APP_COMPOSE="$ROOT_DIR/docker-compose.yml"
LOG_DIR="${GANJI_STACK_LOG_DIR:-/tmp/ganji-local-stack}"
PID_FILE="$LOG_DIR/pids"
POLAR_NODES=(polar-n3-Es polar-n3-Cl polar-n3-Fre)

mkdir -p "$LOG_DIR"

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

port_open() {
  python3 - "$1" <<'PY'
import socket
import sys

sock = socket.socket()
sock.settimeout(0.2)
result = sock.connect_ex(("127.0.0.1", int(sys.argv[1])))
sock.close()
raise SystemExit(0 if result == 0 else 1)
PY
}

wait_for() {
  local description="$1"
  local timeout="$2"
  shift 2
  local started=$SECONDS

  until "$@" >/dev/null 2>&1; do
    if (( SECONDS - started >= timeout )); then
      return 1
    fi
    sleep 2
  done

  log "✓ $description"
}

polar_ready() {
  local container
  for container in polar-n3-backend1 "${POLAR_NODES[@]}"; do
    [ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)" = "true" ] || return 1
  done
}

bitcoind_ready() {
  docker exec -i polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    getblockchaininfo >/dev/null 2>&1
}

lnd_synced() {
  local node="$1"
  local value
  value="$(docker exec -i "$node" lncli \
    --network=regtest \
    --tlscertpath=/home/lnd/.lnd/tls.cert \
    --macaroonpath=/home/lnd/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
    getinfo 2>/dev/null | python3 -c 'import json, sys; print(1 if json.load(sys.stdin).get("synced_to_chain") else 0)' 2>/dev/null || true)"
  [ "$value" = "1" ]
}

all_lnd_synced() {
  local node
  for node in "${POLAR_NODES[@]}"; do
    lnd_synced "$node" || return 1
  done
}

tip_age() {
  local best
  best="$(docker exec -i polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    getbestblockhash 2>/dev/null | python3 -c 'import json, sys; print(json.load(sys.stdin))' 2>/dev/null)" || return 1

  docker exec -i polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    getblockheader "$best" 2>/dev/null | python3 -c 'import json, sys, time; print(int(time.time()) - json.load(sys.stdin)["time"])'
}

mine_block() {
  docker exec -i polar-n3-backend1 bitcoin-cli -regtest \
    -rpcuser=polaruser -rpcpassword=polarpass -rpcport=18443 \
    -generate 1 >/dev/null
}

lnbits_ready() {
  curl -fsS -o /dev/null "http://localhost:$1/"
}

backend_ready() {
  curl -fsS -o /dev/null http://localhost:8080/health
}

local_postgres_ready() {
  python3 - "$ROOT_DIR/backend/.env" <<'PY'
import os
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlparse

env_file = Path(sys.argv[1])
value = None
for line in env_file.read_text().splitlines():
    if line.startswith("DATABASE_URL="):
        value = line.split("=", 1)[1].strip()
        break
if not value:
    raise SystemExit(1)

parsed = urlparse(value)
env = os.environ.copy()
env["PGHOST"] = parsed.hostname or "localhost"
env["PGPORT"] = str(parsed.port or 5432)
env["PGUSER"] = parsed.username or "postgres"
env["PGDATABASE"] = parsed.path.lstrip("/")
if parsed.password:
    env["PGPASSWORD"] = parsed.password

raise SystemExit(0 if subprocess.run(
    ["psql", "-Atc", "select 1"],
    env=env,
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
).returncode == 0 else 1)
PY
}

compose_db_running() {
  local id
  id="$(docker compose -f "$APP_COMPOSE" ps -q db 2>/dev/null || true)"
  [ -n "$id" ] && [ "$(docker inspect -f '{{.State.Running}}' "$id" 2>/dev/null || true)" = "true" ]
}

frontend_ready() {
  curl -fsS -o /dev/null http://localhost:3000/
}

preflight() {
  local command
  for command in docker go npm python3 curl setsid; do
    command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
  done

  [ -f "$POLAR_COMPOSE" ] || fail "Polar compose file not found: $POLAR_COMPOSE"
  [ -f "$LNBITS_COMPOSE" ] || fail "LNbits compose file not found: $LNBITS_COMPOSE"
  [ -f "$APP_COMPOSE" ] || fail "Ganji compose file not found: $APP_COMPOSE"
  [ -f "$ROOT_DIR/backend/.env" ] || fail "missing backend/.env"
  [ -d "$ROOT_DIR/frontend/node_modules" ] || fail "frontend dependencies missing; run npm install in frontend"

  if [ -f "$PID_FILE" ]; then
    local name pid
    while read -r name pid; do
      if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        fail "$name is already running; run make local-stack-stop first"
      fi
    done < "$PID_FILE"
    rm -f "$PID_FILE"
  fi

  if port_open 8080; then
    fail "port 8080 is already in use"
  fi
  if port_open 3000; then
    fail "port 3000 is already in use"
  fi
}

preflight
: > "$PID_FILE"

if port_open 5432; then
  local_postgres_ready || fail "port 5432 is in use but is not the configured Ganji database"
  if compose_db_running; then
    log "Using PostgreSQL from the Ganji db container"
  else
    log "Using existing PostgreSQL on localhost:5432"
    docker compose -f "$APP_COMPOSE" rm -f db >/dev/null 2>&1 || true
  fi
else
  log "Starting PostgreSQL..."
  docker compose -f "$APP_COMPOSE" up -d db
  wait_for "PostgreSQL" 60 docker compose -f "$APP_COMPOSE" exec -T db pg_isready -U postgres -d ganji \
    || fail "PostgreSQL did not become ready"
fi

log "Starting Polar network..."
docker compose -f "$POLAR_COMPOSE" up -d
wait_for "Polar containers" 120 polar_ready || fail "Polar containers did not start"
wait_for "bitcoind RPC" 60 bitcoind_ready || fail "bitcoind RPC did not become ready"

if ! wait_for "LND nodes synced" 30 all_lnd_synced; then
  age="$(tip_age || printf '999999')"
  if [ "$age" -gt 900 ]; then
    log "Refreshing stale regtest tip..."
    mine_block
  fi
  wait_for "LND nodes synced" 120 all_lnd_synced || fail "LND nodes did not sync"
fi

log "Starting LNbits on :5000, :5001, :5002..."
docker compose -f "$LNBITS_COMPOSE" up -d
for port in 5000 5001 5002; do
  wait_for "LNbits on :$port" 90 lnbits_ready "$port" || fail "LNbits on :$port did not start"
done

log "Building backend..."
(cd "$ROOT_DIR/backend" && go build -o "$LOG_DIR/ganji-api" ./cmd/api)

log "Starting backend..."
(
  cd "$ROOT_DIR/backend"
  setsid "$LOG_DIR/ganji-api" >"$LOG_DIR/backend.log" 2>&1 </dev/null &
  printf 'backend %s\n' "$!" >> "$PID_FILE"
)
wait_for "backend /health" 60 backend_ready || {
  sed -n '1,80p' "$LOG_DIR/backend.log" >&2 || true
  fail "backend did not become healthy"
}

log "Starting frontend..."
(
  cd "$ROOT_DIR/frontend"
  setsid node scripts/dev-lan.mjs >"$LOG_DIR/frontend.log" 2>&1 </dev/null &
  printf 'frontend %s\n' "$!" >> "$PID_FILE"
)
wait_for "frontend on :3000" 120 frontend_ready || {
  sed -n '1,80p' "$LOG_DIR/frontend.log" >&2 || true
  fail "frontend did not start"
}

log ""
log "Ganji local stack is ready:"
log "  App:            http://localhost:3000"
log "  Backend:        http://localhost:8080/health"
log "  Escrow LNbits:  http://localhost:5000"
log "  Client LNbits:  http://localhost:5001"
log "  Freelancer:     http://localhost:5002"
log "  Logs:           $LOG_DIR"
log "Stop app processes: make local-stack-stop"

#!/usr/bin/env bash
set -euo pipefail

LOG_DIR="${GANJI_STACK_LOG_DIR:-/tmp/ganji-local-stack}"
PID_FILE="$LOG_DIR/pids"

if [ ! -f "$PID_FILE" ]; then
  printf 'no saved Ganji processes to stop\n'
  exit 0
fi

while read -r name pid; do
  [ -n "$pid" ] || continue
  if kill -0 "$pid" 2>/dev/null; then
    kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
    printf 'stopped %s (%s)\n' "$name" "$pid"
  fi
done < "$PID_FILE"

rm -f "$PID_FILE"

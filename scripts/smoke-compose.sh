#!/usr/bin/env bash
# Build the images, start the compose stack on spare ports, check that the
# Server answers and persists a Project, then tear everything down.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-buildbee-smoke}"
export BUILDBEE_PORT="${BUILDBEE_PORT:-18580}"
export BUILDBEE_WORKER_PORT="${BUILDBEE_WORKER_PORT:-18590}"
export BUILDBEE_FAKE_SANDBOX=1
compose=(docker compose -f "$ROOT/deploy/compose/docker-compose.yml" --profile worker)
trap '"${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

"${compose[@]}" up -d --build --wait
base="http://127.0.0.1:${BUILDBEE_PORT}"
curl -fsS "$base/healthz"
echo
curl -fsS -X POST "$base/v1/projects" -H 'Content-Type: application/json' -H 'X-BuildBee-As: smoke' -d '{"name":"smoke"}' >/dev/null
curl -fsS "$base/v1/projects" | grep -q '"smoke"'
curl -fsS "http://127.0.0.1:${BUILDBEE_WORKER_PORT}/healthz" >/dev/null
echo "smoke ok"

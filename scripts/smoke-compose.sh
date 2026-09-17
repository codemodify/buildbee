#!/usr/bin/env bash
# Build the images, start the compose stack on a spare port, check that the
# Server answers and persists a Project, add a Bot, start its agent (the
# fake AI) and see it finish that Bot's Run. Then tear everything down.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-buildbee-smoke}"
export BUILDBEE_PORT="${BUILDBEE_PORT:-18580}"
compose=(docker compose -f "$ROOT/deploy/compose/docker-compose.yml")
trap '"${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

"${compose[@]}" up -d --build --wait
base="http://127.0.0.1:${BUILDBEE_PORT}"
api() { curl -fsS -H 'Content-Type: application/json' -H 'X-BuildBee-As: smoke' "$@"; }
field() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

curl -fsS "$base/healthz"
echo
project=$(api -X POST "$base/v1/projects" -d '{"name":"smoke"}' | field "['id']")
api "$base/v1/projects" | grep -q '"smoke"'
# A Bot, and its agent: one process, one Bot, one AI.
bot=$(api -X POST "$base/v1/projects/$project/members" \
  -d '{"kind":"bot","display_name":"Smoke","role":"builder","agent":"fake"}' | field "['id']")
BUILDBEE_BOT="$bot" "${compose[@]}" --profile agent up -d --build --wait agent
task=$(api -X POST "$base/v1/projects/$project/tasks" -d '{"title":"smoke run","handoff_role":"none"}' | field "['id']")
run=$(api -X POST "$base/v1/tasks/$task/runs" -d "{\"agent\":\"fake\",\"bot_member_id\":\"$bot\"}" | field "['id']")
for _ in $(seq 60); do
  status=$(api "$base/v1/runs/$run" | field "['status']")
  case "$status" in
    succeeded) echo "run $run succeeded"; break ;;
    failed|canceled) echo "run $run $status" >&2; exit 1 ;;
  esac
  sleep 0.5
done
[ "$status" = succeeded ] || { echo "run $run still $status" >&2; exit 1; }
# Back up and restore: the Project must still be there afterwards.
backup="$(mktemp -d)"
# (the smoke trap removes the stack; this directory is a mktemp)
"$ROOT/scripts/backup.sh" "$backup/b" >/dev/null
test -s "$backup/b/buildbee.dump" && test -s "$backup/b/files.tar.gz"
"$ROOT/scripts/restore.sh" "$backup/b" >/dev/null
BUILDBEE_BOT="$bot" "${compose[@]}" up -d --wait >/dev/null
api "$base/v1/projects" | grep -q '"smoke"'
echo "backup and restore ok"

echo "smoke ok"

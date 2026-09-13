#!/usr/bin/env bash
# Create Project → Channel message → Task → Handoff → Decision answer → Run.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "health: $(curl -fsS "$BASE/healthz")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"E2E Hive"}')
pid=$(printf '%s' "$proj" | json "['id']")
human=$(printf '%s' "$proj" | json "['members'][0]['id']")
bot=$(printf '%s' "$proj" | json "['members'][1]['id']")
chan=$(printf '%s' "$proj" | json "['channels'][0]['id']")
echo "project=$pid"

curl -fsS -X POST "$BASE/v1/channels/$chan/messages" -H 'Content-Type: application/json' \
  -d "{\"body\":\"hello from e2e\",\"member_id\":\"$human\"}" >/dev/null

task=$(curl -fsS -X POST "$BASE/v1/projects/$pid/tasks" -H 'Content-Type: application/json' \
  -d '{"title":"Ship the slice"}')
tid=$(printf '%s' "$task" | json "['id']")
echo "task=$tid"

ho=$(curl -fsS -X POST "$BASE/v1/tasks/$tid/handoffs" -H 'Content-Type: application/json' \
  -d "{\"from_member_id\":\"$human\",\"to_member_id\":\"$bot\",\"note\":\"please take this\"}")
hid=$(printf '%s' "$ho" | json "['id']")
curl -fsS -X POST "$BASE/v1/handoffs/$hid/complete" >/dev/null

dec=$(curl -fsS -X POST "$BASE/v1/projects/$pid/decisions" -H 'Content-Type: application/json' \
  -d '{"prompt":"Ship today?","options":["yes","no"],"recommendation":"yes"}')
did=$(printf '%s' "$dec" | json "['id']")
curl -fsS -X POST "$BASE/v1/decisions/$did/answer" -H 'Content-Type: application/json' \
  -d '{"answer":"yes"}' >/dev/null

run=$(curl -fsS -X POST "$BASE/v1/tasks/$tid/runs")
rid=$(printf '%s' "$run" | json "['id']")
curl -fsS -X PATCH "$BASE/v1/runs/$rid" -H 'Content-Type: application/json' \
  -d '{"status":"succeeded","detail":"fake success"}' >/dev/null

echo "activity types:"
curl -fsS "$BASE/v1/projects/$pid/activity" | python3 -c "import json,sys; print(sorted({i['type'] for i in json.load(sys.stdin)['items']}))"
echo "e2e ok project=$pid"

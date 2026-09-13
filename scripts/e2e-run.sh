#!/usr/bin/env bash
# Run → Artifact (log + fake PR) → Pipelines webhook.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Run Hive"}')
pid=$(printf '%s' "$proj" | json "['id']")
task=$(curl -fsS -X POST "$BASE/v1/projects/$pid/tasks" -H 'Content-Type: application/json' -d '{"title":"Sandbox slice"}')
tid=$(printf '%s' "$task" | json "['id']")
echo "project=$pid task=$tid"

run=$(curl -fsS -X POST "$BASE/v1/tasks/$tid/runs")
rid=$(printf '%s' "$run" | json "['id']")
curl -fsS -X PATCH "$BASE/v1/runs/$rid" -H 'Content-Type: application/json' \
  -d '{"status":"running","detail":"sandbox"}' >/dev/null
curl -fsS -X PATCH "$BASE/v1/runs/$rid" -H 'Content-Type: application/json' \
  -d '{"status":"succeeded","detail":"fake sandbox"}' >/dev/null

curl -fsS -X POST "$BASE/v1/tasks/$tid/artifacts" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"log\",\"name\":\"sandbox.log\",\"body\":\"fake sandbox ok\\n\",\"run_id\":\"$rid\"}" >/dev/null

pr=$(curl -fsS -X POST "$BASE/v1/tasks/$tid/pr" -H 'Content-Type: application/json' \
  -d "{\"fake\":true,\"run_id\":\"$rid\"}")
echo "pr=$(printf '%s' "$pr" | json "['pr']['url']")"

curl -fsS -X POST "$BASE/v1/pipelines/webhook" -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$tid\",\"name\":\"ci\",\"status\":\"success\",\"external_url\":\"https://example.test/actions/1\"}" >/dev/null

# GitHub-shaped payload also accepted
curl -fsS -X POST "$BASE/v1/pipelines/webhook" -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$tid\",\"check_run\":{\"name\":\"test\",\"conclusion\":\"success\",\"html_url\":\"https://github.com/example/buildbee/actions/2\"}}" >/dev/null

detail=$(curl -fsS "$BASE/v1/tasks/$tid/detail")
echo "$detail" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['runs'] and d['artifacts'] and d['pipelines']; print('runs',len(d['runs']),'artifacts',len(d['artifacts']),'pipelines',len(d['pipelines']))"
echo "e2e-run ok task=$tid"

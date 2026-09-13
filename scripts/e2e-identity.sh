#!/usr/bin/env bash
# Dev auth + fake Issues→Task + Routine force-run.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"
json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "auth: $(curl -fsS "$BASE/v1/auth/me")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Identity Hive"}')
pid=$(printf '%s' "$proj" | json "['id']")
echo "project=$pid"

sync=$(curl -fsS -X POST "$BASE/v1/projects/$pid/issues/sync" -H 'Content-Type: application/json' -d '{"fake":true}')
echo "$sync" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['fake'] and len(d['items'])>=2; print('issues',len(d['items']))"

curl -fsS -X POST "$BASE/v1/issues/webhook?project_id=$pid" -H 'Content-Type: application/json' \
  -d '{"action":"opened","issue":{"number":42,"title":"Webhook issue","html_url":"https://github.com/example/buildbee/issues/42"}}' >/dev/null

rts=$(curl -fsS "$BASE/v1/projects/$pid/routines")
rid=$(printf '%s' "$rts" | json "['items'][0]['id']")
echo "routine=$rid"
curl -fsS -X POST "$BASE/v1/routines/$rid/run" >/dev/null

curl -fsS "$BASE/v1/projects/$pid/activity?type=routine" | python3 -c "import json,sys; assert json.load(sys.stdin)['items']"
echo "e2e-identity ok project=$pid"

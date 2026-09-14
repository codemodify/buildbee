#!/usr/bin/env bash
# Notifications: Decision / Handoff / @mention / Pipeline failure → inbox → read.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "health: $(curl -fsS "$BASE/healthz")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Notify Project"}')
pid=$(printf '%s' "$proj" | json "['id']")
human=$(printf '%s' "$proj" | python3 -c "import json,sys; p=json.load(sys.stdin); print(next(m['id'] for m in p['members'] if m['kind']=='human'))")
scout=$(printf '%s' "$proj" | python3 -c "import json,sys; p=json.load(sys.stdin); print(next(m['id'] for m in p['members'] if m.get('role')=='scout'))")
chan=$(printf '%s' "$proj" | json "['channels'][0]['id']")
echo "project=$pid human=$human"

curl -fsS -X POST "$BASE/v1/projects/$pid/decisions" -H 'Content-Type: application/json' \
  -d '{"prompt":"Need a Decision?","options":["yes","no"],"recommendation":"yes"}' >/dev/null

task=$(curl -fsS -X POST "$BASE/v1/projects/$pid/tasks?handoff=none" -H 'Content-Type: application/json' \
  -d '{"title":"Owned Task"}')
tid=$(printf '%s' "$task" | json "['id']")

curl -fsS -X POST "$BASE/v1/tasks/$tid/handoffs" -H 'Content-Type: application/json' \
  -d "{\"from_member_id\":\"$scout\",\"to_member_id\":\"$human\",\"note\":\"back to you\"}" >/dev/null

curl -fsS -X POST "$BASE/v1/channels/$chan/messages" -H 'Content-Type: application/json' \
  -d "{\"body\":\"@Scout please look\",\"member_id\":\"$human\"}" >/dev/null

curl -fsS -X POST "$BASE/v1/pipelines/webhook" -H 'Content-Type: application/json' \
  -d "{\"task_id\":\"$tid\",\"name\":\"ci\",\"status\":\"failure\"}" >/dev/null

inbox=$(curl -fsS "$BASE/v1/notifications?member_id=$human")
nid=$(printf '%s' "$inbox" | python3 -c "
import json,sys
d=json.load(sys.stdin)
kinds={i['kind'] for i in d['items']}
need={'decision','handoff','mention','pipeline'}
missing=need-kinds
assert not missing, (missing, d)
assert d['unread']>=4, d
print('inbox unread=%s kinds=%s' % (d['unread'], sorted(kinds)), file=sys.stderr)
print(d['items'][0]['id'])
")

readone=$(curl -fsS -X POST "$BASE/v1/notifications/$nid/read")
printf '%s' "$readone" | python3 -c "import json,sys; n=json.load(sys.stdin); assert n.get('read_at'), n"

curl -fsS -X POST "$BASE/v1/notifications/read-all?member_id=$human" >/dev/null
after=$(curl -fsS "$BASE/v1/notifications?member_id=$human&unread=1")
printf '%s' "$after" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['unread']==0 and d['items']==[], d"

echo "e2e-notifications ok project=$pid"

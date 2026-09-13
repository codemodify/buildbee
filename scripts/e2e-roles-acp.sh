#!/usr/bin/env bash
# Seeded Bot Roles (Scout/Builder/Sentry/Pulse) + FakeACP Run.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }
member_id() {
  python3 -c "import json,sys
p=json.load(sys.stdin)
role=sys.argv[1]
for m in p['members']:
    if m.get('role')==role:
        print(m['id']); break
" "$1"
}

echo "health: $(curl -fsS "$BASE/healthz")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Roles ACP Hive"}')
pid=$(printf '%s' "$proj" | json "['id']")
human=$(printf '%s' "$proj" | member_id owner)
scout=$(printf '%s' "$proj" | member_id scout)
builder=$(printf '%s' "$proj" | member_id builder)
sentry=$(printf '%s' "$proj" | member_id sentry)
pulse=$(printf '%s' "$proj" | member_id pulse)
if [ -z "$scout" ] || [ -z "$builder" ] || [ -z "$sentry" ] || [ -z "$pulse" ]; then
  echo "missing seeded Bot Roles" >&2
  echo "$proj" | python3 -m json.tool >&2
  exit 1
fi
echo "project=$pid scout=$scout builder=$builder sentry=$sentry pulse=$pulse"

printf '%s' "$proj" | python3 -c "
import json,sys
p=json.load(sys.stdin)
roles={m['role'] for m in p['members'] if m['kind']=='bot'}
assert roles=={'scout','builder','sentry','pulse'}, roles
assert all(m.get('instructions') for m in p['members'] if m['kind']=='bot')
print('roles ok', sorted(roles))
"

task=$(curl -fsS -X POST "$BASE/v1/projects/$pid/tasks?handoff=scout" -H 'Content-Type: application/json' \
  -d '{"title":"Scope unclear?"}')
tid=$(printf '%s' "$task" | json "['id']")
hid=$(printf '%s' "$task" | json "['handoff']['id']")
echo "task=$tid scout_handoff=$hid"

done=$(curl -fsS -X POST "$BASE/v1/handoffs/$hid/complete")
printf '%s' "$done" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('decision'), d; print('scout decision', d['decision']['id'])"

ho=$(curl -fsS -X POST "$BASE/v1/tasks/$tid/handoffs?autorun=1" -H 'Content-Type: application/json' \
  -d "{\"from_member_id\":\"$human\",\"to_role\":\"builder\",\"note\":\"implement\"}")
printf '%s' "$ho" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('run'), d; print('builder autorun', d['run']['id'])"

export BUILDBEE_URL="$BASE"
(cd "$ROOT/cli" && GOTOOLCHAIN=local go run ./cmd/buildbee run start --task "$tid" --acp --agent fake) | python3 -c "
import json,sys
d=json.load(sys.stdin)
art=d.get('artifact') or {}
assert art.get('name')=='acp.log' or 'acp' in json.dumps(d).lower(), d
print('fake acp run ok', art.get('name'), d.get('acp_agent'))
"

detail=$(curl -fsS "$BASE/v1/tasks/$tid/detail")
echo "$detail" | python3 -c "
import json,sys
d=json.load(sys.stdin)
names={a['name'] for a in d.get('artifacts',[])}
assert 'acp.log' in names, names
print('artifacts', sorted(names))
"
chan=$(printf '%s' "$proj" | json "['channels'][0]['id']")
mention=$(curl -fsS -X POST "$BASE/v1/channels/$chan/messages" -H 'Content-Type: application/json' \
  -d "{\"body\":\"@Scout please triage from Channel\",\"member_id\":\"$human\"}")
printf '%s' "$mention" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('mentions') and d.get('tasks'); print('mention', d['mentions'][0]['role'], d['tasks'][0]['id'])"

echo "e2e-roles-acp ok project=$pid task=$tid"

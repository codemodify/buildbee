#!/usr/bin/env bash
# Member Invite: create → preview → accept → revoke + permissions.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "health: $(curl -fsS "$BASE/healthz")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Invite Hive"}')
pid=$(printf '%s' "$proj" | json "['id']")
owner=$(printf '%s' "$proj" | python3 -c "import json,sys; p=json.load(sys.stdin); print(next(m['id'] for m in p['members'] if m.get('role')=='owner'))")
echo "project=$pid owner=$owner"

inv=$(curl -fsS -X POST "$BASE/v1/projects/$pid/invites" -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.test","github_login":"ada","role":"admin"}')
token=$(printf '%s' "$inv" | json "['token']")
path=$(printf '%s' "$inv" | json "['path']")
iid=$(printf '%s' "$inv" | json "['id']")
python3 -c "import sys; t,p=sys.argv[1:3]; assert t and p.startswith('#/invite/'), (t,p)" "$token" "$path"
echo "invite=$iid token_ok path=$path"

preview=$(curl -fsS "$BASE/v1/invites/$token")
printf '%s' "$preview" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['status']=='pending' and d['project_name']=='Invite Hive', d"

pending=$(curl -fsS "$BASE/v1/projects/$pid/invites")
printf '%s' "$pending" | python3 -c "import json,sys; d=json.load(sys.stdin); assert len(d['items'])==1, d"

acc=$(curl -fsS -X POST "$BASE/v1/invites/$token/accept" -H 'Content-Type: application/json' \
  -d '{"display_name":"Ada","github_login":"ada"}')
printf '%s' "$acc" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('already_member') is False, d
assert d['member']['display_name']=='Ada' and d['member']['role']=='admin', d
print('joined', d['member']['id'])
"

members=$(curl -fsS "$BASE/v1/projects/$pid/members")
printf '%s' "$members" | python3 -c "
import json,sys
d=json.load(sys.stdin)
names={m['display_name'] for m in d['items'] if m['kind']=='human'}
assert 'Ada' in names, names
"

bob=$(curl -fsS -X POST "$BASE/v1/projects/$pid/members" -H 'Content-Type: application/json' \
  -d '{"display_name":"Bob","kind":"human","role":"member"}')
bob_id=$(printf '%s' "$bob" | json "['id']")
code=$(curl -sS -o /tmp/invite-forbidden.json -w '%{http_code}' -X POST "$BASE/v1/projects/$pid/invites" \
  -H 'Content-Type: application/json' -H "X-Member-ID: $bob_id" \
  -d '{"email":"eve@example.test"}')
test "$code" = "403"

curl -fsS "$BASE/v1/projects/$pid/invites" -H "X-Member-ID: $bob_id" >/dev/null

inv2=$(curl -fsS -X POST "$BASE/v1/projects/$pid/invites" -H 'Content-Type: application/json' \
  -d '{"email":"eve@example.test","role":"member"}')
id2=$(printf '%s' "$inv2" | json "['id']")
tok2=$(printf '%s' "$inv2" | json "['token']")
curl -fsS -X DELETE "$BASE/v1/invites/$id2" >/dev/null
code=$(curl -sS -o /tmp/invite-revoked.json -w '%{http_code}' -X POST "$BASE/v1/invites/$tok2/accept" \
  -H 'Content-Type: application/json' -d '{"display_name":"Eve"}')
test "$code" = "409"

echo "e2e-invites ok project=$pid"

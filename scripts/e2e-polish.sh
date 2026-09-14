#!/usr/bin/env bash
# Polish: shared GitHub Identity, Decision assignee notify, mute prefs.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "health: $(curl -fsS "$BASE/healthz")"

a=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Alpha"}')
b=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"Beta"}')
aid=$(printf '%s' "$a" | json "['id']")
bid=$(printf '%s' "$b" | json "['id']")
invA=$(curl -fsS -X POST "$BASE/v1/projects/$aid/invites" -H 'Content-Type: application/json' -d '{"github_login":"ada","role":"member"}')
invB=$(curl -fsS -X POST "$BASE/v1/projects/$bid/invites" -H 'Content-Type: application/json' -d '{"github_login":"Ada","role":"admin"}')
tokA=$(printf '%s' "$invA" | json "['token']")
tokB=$(printf '%s' "$invB" | json "['token']")
memA=$(curl -fsS -X POST "$BASE/v1/invites/$tokA/accept" -H 'Content-Type: application/json' -d '{"display_name":"Ada","github_login":"ada"}')
memB=$(curl -fsS -X POST "$BASE/v1/invites/$tokB/accept" -H 'Content-Type: application/json' -d '{"display_name":"Ada Lovelace","github_login":"ADA"}')
python3 -c "
import json,sys
a=json.loads(sys.argv[1])['member']
b=json.loads(sys.argv[2])['member']
assert a['identity'] and a['identity']==b['identity'], (a,b)
assert a['display_name']==b['display_name'], (a,b)
print('shared identity', a['identity'], a['display_name'])
" "$memA" "$memB"

ada=$(printf '%s' "$memA" | json "['member']['id']")
owner=$(printf '%s' "$a" | python3 -c "import json,sys; p=json.load(sys.stdin); print(next(m['id'] for m in p['members'] if m.get('role')=='owner'))")
dec=$(curl -fsS -X POST "$BASE/v1/projects/$aid/decisions" -H 'Content-Type: application/json' \
  -d "{\"prompt\":\"Only Ada?\",\"options\":[\"yes\"],\"assignee_id\":\"$ada\"}")
printf '%s' "$dec" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('assignee_id'), d"
ownerN=$(curl -fsS "$BASE/v1/notifications?member_id=$owner")
printf '%s' "$ownerN" | python3 -c "import json,sys; d=json.load(sys.stdin); assert not any(i['kind']=='decision' for i in d['items']), d"
adaN=$(curl -fsS "$BASE/v1/notifications?member_id=$ada")
printf '%s' "$adaN" | python3 -c "import json,sys; d=json.load(sys.stdin); assert any(i['kind']=='decision' for i in d['items']), d"
echo "assignee notify ok"

curl -fsS -X PATCH "$BASE/v1/me/preferences?member_id=$owner" -H 'Content-Type: application/json' \
  -d '{"mute_mentions":true}' >/dev/null
chan=$(printf '%s' "$a" | json "['channels'][0]['id']")
curl -fsS -X POST "$BASE/v1/channels/$chan/messages" -H 'Content-Type: application/json' \
  -d "{\"body\":\"@Scout muted\",\"member_id\":\"$owner\"}" >/dev/null
after=$(curl -fsS "$BASE/v1/notifications?member_id=$owner")
printf '%s' "$after" | python3 -c "import json,sys; d=json.load(sys.stdin); assert not any(i['kind']=='mention' for i in d['items']), d"
echo "mute mentions ok"

echo "e2e-polish ok"

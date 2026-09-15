#!/usr/bin/env bash
# FakeACP live stream: ≥N RunEvents arrive while the Run is still running.
set -euo pipefail
BASE="${BUILDBEE_URL:-http://127.0.0.1:8080}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NEEDLE="${ACP_STREAM_MIN_EVENTS:-5}"

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "health: $(curl -fsS "$BASE/healthz")"

proj=$(curl -fsS -X POST "$BASE/v1/projects" -H 'Content-Type: application/json' -d '{"name":"ACP Stream Project"}')
pid=$(printf '%s' "$proj" | json "['id']")
task=$(curl -fsS -X POST "$BASE/v1/projects/$pid/tasks?handoff=none" -H 'Content-Type: application/json' \
  -d '{"title":"Stream the FakeACP Run"}')
tid=$(printf '%s' "$task" | json "['id']")
echo "project=$pid task=$tid"

export BUILDBEE_URL="$BASE"
(cd "$ROOT" && GOTOOLCHAIN=local go run ./cmd/buildbee run start --task "$tid" --acp --agent fake) \
  >/tmp/buildbee-acp-stream-out.json 2>/tmp/buildbee-acp-stream-err.log &
clipid=$!

seen_live=0
rid=""
for i in $(seq 1 50); do
  if [ -z "$rid" ]; then
    detail=$(curl -fsS "$BASE/v1/tasks/$tid/detail" || true)
    rid=$(printf '%s' "$detail" | python3 -c "import json,sys
d=json.load(sys.stdin)
runs=d.get('runs') or []
print(runs[0]['id'] if runs else '')" 2>/dev/null || true)
  fi
  if [ -n "$rid" ]; then
    ev=$(curl -fsS "$BASE/v1/runs/$rid/events" || true)
    python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d.get('items') or []
n=len(items)
status=''
# last status event
for e in items:
    if e.get('kind')=='status' and isinstance(e.get('payload'), dict):
        status=str(e['payload'].get('status') or '')
open('/tmp/buildbee-acp-stream-count','w').write(str(n))
open('/tmp/buildbee-acp-stream-status','w').write(status)
print(f'poll n={n} last_status={status}')
" <<<"$ev"
    n=$(cat /tmp/buildbee-acp-stream-count 2>/dev/null || echo 0)
    st=$(cat /tmp/buildbee-acp-stream-status 2>/dev/null || echo "")
    if [ "$n" -ge "$NEEDLE" ] && [ "$st" != "succeeded" ] && [ "$st" != "failed" ]; then
      seen_live=1
      echo "saw $n events before Run completed"
      break
    fi
  fi
  if ! kill -0 "$clipid" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

wait "$clipid" || true
echo "cli stdout: $(head -c 400 /tmp/buildbee-acp-stream-out.json)"
echo "cli stderr: $(head -c 400 /tmp/buildbee-acp-stream-err.log)"

if [ -z "$rid" ]; then
  rid=$(python3 -c "import json; print(json.load(open('/tmp/buildbee-acp-stream-out.json'))['run']['id'])")
fi
final=$(curl -fsS "$BASE/v1/runs/$rid/events")
python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d.get('items') or []
kinds={e['kind'] for e in items}
print('final events', len(items), sorted(kinds))
assert len(items) >= int(sys.argv[1]), items
assert 'token' in kinds and 'tool_call' in kinds, kinds
" "$NEEDLE" <<<"$final"

if [ "$seen_live" != 1 ]; then
  echo "did not observe ≥$NEEDLE events while Run was still running" >&2
  exit 1
fi

detail=$(curl -fsS "$BASE/v1/tasks/$tid/detail")
printf '%s' "$detail" | python3 -c "
import json,sys
d=json.load(sys.stdin)
names={a['name'] for a in d.get('artifacts',[])}
assert 'acp.log' in names, names
runs=d.get('runs') or []
assert runs and runs[0]['status']=='succeeded', runs
print('acp.log + succeeded ok')
"

echo "e2e-acp-stream ok project=$pid task=$tid run=$rid"

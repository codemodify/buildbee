#!/usr/bin/env bash
# Mirror GitHub Actions CI locally (memory store, no Docker).
set -euo pipefail
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "== go test server =="
(cd server && go test ./...)
echo "== go test cli =="
(cd cli && go test ./...)
echo "== go test runtime =="
(cd runtime && go test ./...)
echo "== web build =="
(cd web && npm ci && npm run build)

echo "== e2e (memory Server) =="
need_stop=0
if ! curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  (cd server && go run ./cmd/server) >/tmp/buildbee-server-ci.log 2>&1 &
  echo $! > /tmp/buildbee-server-ci.pid
  need_stop=1
  for i in $(seq 1 45); do
    if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
fi
export BUILDBEE_URL="${BUILDBEE_URL:-http://127.0.0.1:8080}"
./scripts/e2e.sh
./scripts/e2e-roles-acp.sh
./scripts/e2e-notifications.sh
./scripts/e2e-invites.sh
./scripts/e2e-polish.sh
if [ "$need_stop" = 1 ] && [ -f /tmp/buildbee-server-ci.pid ]; then
  kill "$(cat /tmp/buildbee-server-ci.pid)" >/dev/null 2>&1 || true
fi
echo "ci-local ok"

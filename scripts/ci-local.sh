#!/usr/bin/env bash
# Mirror GitHub Actions CI locally. Needs Docker for a throwaway Postgres.
set -euo pipefail
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "== go test =="
go test ./...
echo "== web build =="
(cd web && npm ci && npm run build)

if command -v cargo >/dev/null 2>&1 && (pkg-config --exists webkit2gtk-4.1 || pkg-config --exists webkit2gtk-4.0); then
  echo "== desktop cargo check =="
  (cd desktop/src-tauri && cargo check && cargo test)
else
  echo "== desktop cargo check skipped (need cargo + webkit2gtk) =="
fi

echo "== e2e (Postgres Server) =="
need_stop=0
pg=""
if ! curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  pg="$(docker run -d --rm -e POSTGRES_PASSWORD=postgres -p 127.0.0.1::5432 postgres:17-alpine)"
  trap 'docker rm -f "$pg" >/dev/null 2>&1 || true' EXIT
  pgport="$(docker port "$pg" 5432/tcp | head -1 | cut -d: -f2)"
  export DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${pgport}/postgres?sslmode=disable"
  go run ./cmd/buildbee-server >/tmp/buildbee-server-ci.log 2>&1 &
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
./scripts/e2e-acp-stream.sh
if [ "$need_stop" = 1 ] && [ -f /tmp/buildbee-server-ci.pid ]; then
  kill "$(cat /tmp/buildbee-server-ci.pid)" >/dev/null 2>&1 || true
fi
echo "ci-local ok"

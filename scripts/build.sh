#!/usr/bin/env bash
# Build the Vite web app, copy dist into the Server embed dir, then compile the Server.
# Same-origin: the binary serves /v1 + /healthz + the SPA (or set BUILDBEE_WEB_DIR).
set -euo pipefail
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

(cd web && npm ci && npm run build)

rm -rf server/internal/webui/dist
mkdir -p server/internal/webui/dist
cp -a web/dist/. server/internal/webui/dist/
touch server/internal/webui/dist/.gitkeep

mkdir -p bin
(cd server && go build -o "$ROOT/bin/buildbee-server" ./cmd/server)
echo "built $ROOT/bin/buildbee-server (web UI embedded)"
echo "run: ./bin/buildbee-server"
echo "or:  BUILDBEE_WEB_DIR=$ROOT/web/dist (cd server && go run ./cmd/server)"

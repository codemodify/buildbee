#!/usr/bin/env bash
# Restore a backup made by scripts/backup.sh into a running compose stack.
# The database is restored over what is there; the files are unpacked into
# the Server's volume.
#
#   scripts/restore.sh DIR
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
dir="${1:?usage: scripts/restore.sh DIR}"
compose=(docker compose -f "$ROOT/deploy/compose/docker-compose.yml")

"${compose[@]}" exec -T postgres pg_restore -U buildbee -d buildbee --clean --if-exists < "$dir/buildbee.dump"
volume="$("${compose[@]}" config --format json | python3 -c "import json,sys; print(json.load(sys.stdin)['volumes']['server_data'].get('name') or '')")"
volume="${volume:-${COMPOSE_PROJECT_NAME:-compose}_server_data}"
docker run --rm -v "$volume:/data" -v "$dir:/backup:ro" alpine:3.24 sh -c "rm -rf /data/* && tar -xzf /backup/files.tar.gz -C /data"
"${compose[@]}" restart server
echo "restored from $dir"

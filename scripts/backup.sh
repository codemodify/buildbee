#!/usr/bin/env bash
# Back up a compose deployment: the database and the files (attachments and
# large Artifacts). Both land in one directory, named by the date.
#
#   scripts/backup.sh [DIR]          # default: ./backups/<date>
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$ROOT/backups/$(date +%F-%H%M%S)}"
compose=(docker compose -f "$ROOT/deploy/compose/docker-compose.yml")
mkdir -p "$out"

"${compose[@]}" exec -T postgres pg_dump -U buildbee -Fc buildbee > "$out/buildbee.dump"
# The Server's /data volume, through a throwaway container so it works
# whether or not the Server is running.
volume="$("${compose[@]}" config --format json | python3 -c "import json,sys; print(json.load(sys.stdin)['volumes']['server_data'].get('name') or '')")"
volume="${volume:-${COMPOSE_PROJECT_NAME:-compose}_server_data}"
docker run --rm -v "$volume:/data:ro" -v "$out:/backup" alpine:3.24 tar -czf /backup/files.tar.gz -C /data .
echo "backup in $out"
ls -l "$out"

# Deploy on a LAN

BuildBee runs as one Server on a trusted network, plus one `buildbee-agent` process per Bot, on the machines where those AIs are logged in. There is no login: anyone who can reach the Server can use every Project.

## Server

On the host that will serve BuildBee:

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

This starts Postgres and the Server. The Server applies migrations at startup and serves the API and web UI on port 8080 (`BUILDBEE_PORT` changes the published port). Postgres is not published outside the compose network.

Compose reads these from the environment or an `.env` file next to the compose file:

| Variable | Default | Notes |
| --- | --- | --- |
| `BUILDBEE_PORT` | `8080` | port the Server is published on |
| `BUILDBEE_DB_PASSWORD` | `buildbee` | Postgres password; set it before the first start |
| `GITHUB_TOKEN`, `GITHUB_REPO` | unset | Issue sync (agents open PRs with their own `gh` login) |
| `GITHUB_WEBHOOK_SECRET` | unset | require signed GitHub webhooks |

`/healthz` returns 200 only when Postgres answers; compose uses it as the Server's health check.

## Workers

Agents connect out to the Server and open no port. Add a Bot in `# status`, then start its agent with the line that dialog shows. The compose `agent` profile starts one running the fake AI, for demos:

```bash
BUILDBEE_BOT=<bot id> docker compose -f deploy/compose/docker-compose.yml --profile agent up -d --build
```

A Bot that runs a real AI needs its agent on a machine where that CLI is installed and logged in. See [docs/agents.md](../docs/agents.md).

## Backups

Two things hold state: the database (`postgres_data`) and the files (`server_data`: attachments and large Artifacts). One script takes both:

```bash
scripts/backup.sh                 # into ./backups/<date>
scripts/restore.sh backups/2026-09-15-120000
```

The backup is a `pg_dump` archive plus a tar of the files volume; restoring replaces both and restarts the Server. The smoke test backs up and restores on every run, so the scripts stay honest. Back up before every upgrade, and keep a copy off the machine.

## Monitoring

`GET /metrics` is Prometheus text: Runs by status, Runs running and queued per Bot, Bots connected and their slots, people online, and request counts and latency. `# status` shows the same at a glance: which Bots are online, what is running, and what failed today.

Finished Runs' event streams (the agents' token-by-token output) and read notifications are deleted after `BUILDBEE_RETENTION_DAYS` (90 by default; 0 keeps them). The Runs themselves, their summaries, Artifacts and messages are never swept.

## Files

Attachments and large Artifacts (agent logs, diffs over 64 KiB) are kept outside Postgres, in the `server_data` volume (`BUILDBEE_BLOB_DIR=/data/blobs`). To keep them in an S3-compatible service instead (MinIO, Garage, SeaweedFS, AWS S3), set `BUILDBEE_S3_ENDPOINT`, `BUILDBEE_S3_ACCESS_KEY` and `BUILDBEE_S3_SECRET_KEY` (and `BUILDBEE_S3_BUCKET`, default `buildbee`, created at startup if missing). One attachment is at most `BUILDBEE_MAX_UPLOAD_BYTES` (25 MiB).

## Upgrades

Pull, rebuild and restart. The Server applies new migrations at startup, each in its own transaction, under a lock so two Servers cannot race. Released migrations never change; a database made by a newer BuildBee is refused rather than guessed at. Back up first (above).

```bash
git pull
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

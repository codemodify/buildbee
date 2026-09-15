# Deploy on a LAN

BuildBee runs as one Server on a trusted network, plus workers on the machines that execute Runs. There is no login: anyone who can reach the Server can use every Project.

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
| `GITHUB_TOKEN`, `GITHUB_REPO` | unset | draft PRs and Issue sync |
| `GITHUB_WEBHOOK_SECRET` | unset | require signed GitHub webhooks |

`/healthz` returns 200 only when Postgres answers; compose uses it as the Server's health check.

## Workers

A worker executes Runs next to a Docker daemon. On a machine dedicated to Runs:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile worker up -d --build
```

The worker mounts the Docker socket, which is root-equivalent on that host. Its Run endpoint is bound to `127.0.0.1` (`BUILDBEE_WORKER_PORT`, default 8090). `BUILDBEE_FAKE_SANDBOX=1` runs the fake engine instead of Docker, for demos and tests only.

The worker currently receives Runs from the CLI (`buildbee run start`). Pulling Runs from a queue on the Server, so any number of workers can serve many Projects, is part of the [roadmap](../docs/roadmap.md).

## Backups

Everything lives in the `postgres_data` volume. Back it up with `pg_dump` before every upgrade:

```bash
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  pg_dump -U buildbee -Fc buildbee > buildbee-$(date +%F).dump
```

Restore into an empty database:

```bash
docker compose -f deploy/compose/docker-compose.yml exec -T postgres \
  pg_restore -U buildbee -d buildbee --clean --if-exists < buildbee-2026-09-15.dump
```

## Upgrades

Until the first release, the schema is one baseline migration that is edited in place. When it changes, the Server refuses to start against an older database and says so. Recreate the database:

```bash
docker compose -f deploy/compose/docker-compose.yml down -v
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

After the first release every schema change is a new migration, applied automatically at startup under a lock.

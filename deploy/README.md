# Deploy

**Self-host source of truth:** [compose/](compose/) (Postgres 16 + Server, optional runtime / MinIO).

## Compose (self-host)

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

See [compose/README.md](compose/README.md).

## Railway

Provision a **Postgres** plugin and a service that builds this repo with the root [`Dockerfile`](../Dockerfile) (or [`server/Dockerfile`](../server/Dockerfile) if the service root is `server/`).

[`railway.toml`](railway.toml) / repo-root [`railway.toml`](../railway.toml):

- Health check: `GET /healthz`
- Server listens on `PORT` (Railway) or `BUILDBEE_ADDR` (Compose default `:8080`)
- On boot, if `DATABASE_URL` is set, the Server applies SQL migrations

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | yes on Railway | Railway Postgres. Unset = in-memory (dev only) |
| `PORT` | set by Railway | |
| `SESSION_SECRET` | with OAuth | cookie signing |
| `GITHUB_CLIENT_ID` | no | omit = dev auth |
| `GITHUB_CLIENT_SECRET` | with OAuth | |
| `GITHUB_OAUTH_REDIRECT` | with OAuth | `https://<domain>/v1/auth/callback` |
| `GITHUB_TOKEN` | no | draft PRs / Issues sync |
| `GITHUB_REPO` | no | `owner/name` |
| `GITHUB_WEBHOOK_SECRET` | no | `X-Hub-Signature-256` |
| `BUILDBEE_FRONTEND_URL` | no | web origin if split from the Server |

The **web** app can stay on Vite locally, or be served as static files behind any host. The Server does not embed the UI.

```bash
# from repo root, after `railway login` / linking a project
railway up
```

Runtime (ACP / Docker Sandbox) is a separate process (`runtime/`). Do not expect DinD on Railway; use FakeACP / `--fake` or run the supervisor on a host with Docker.

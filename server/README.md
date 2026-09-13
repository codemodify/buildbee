# Server

Go **Server** for BuildBee: REST + WebSocket skeleton.

Module: `github.com/codemodify/buildbee/server`

## Run

```bash
go test ./...
go run ./cmd/server
```

Listens on `:8080` by default. Set `BUILDBEE_ADDR` to override.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/healthz` | Liveness. Does not require Postgres yet. |
| GET | `/v1/` | Placeholder API router (Project, Channel, Task, Decisions later). |
| GET | `/v1/ws` | WebSocket hub stub (`501` until implemented). |

## Auth (stub)

- Humans: GitHub OAuth later.
- Bots: server-issued Identities.

## Docker

From the repo root:

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

Or build this directory directly:

```bash
docker build -t buildbee-server .
```

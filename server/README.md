# Server

Go **Server** for BuildBee. Module: `github.com/codemodify/buildbee/server`

Persists **Project**, **Member**, **Channel**, **Message**, **Task**, **Handoff**, **Decision**, **Activity**, and **Run** records. Migrations run on startup when `DATABASE_URL` is set.

## Run

```bash
go test ./...
go run ./cmd/server                          # in-memory Store
DATABASE_URL=postgres://... go run ./cmd/server
```

`GET /healthz` is liveness (does not require Postgres).

## `/v1` APIs

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/v1/` | Index |
| GET/POST | `/v1/projects` | List / create Project (seeds human + Bot Members and `#general`) |
| GET | `/v1/projects/{id}` | Project + Members + Channels |
| GET/POST | `/v1/projects/{id}/members` | List / add Member (`kind`: human \| bot) |
| GET/POST | `/v1/projects/{id}/channels` | List / create Channel |
| GET/POST | `/v1/channels/{id}/messages` | List / post Message |
| GET | `/v1/channels/{id}/ws` | WebSocket: pushes new Channel messages |
| GET/POST | `/v1/projects/{id}/tasks` | List / create Task |
| GET/PATCH | `/v1/tasks/{id}` | Get / update status |
| POST | `/v1/tasks/{id}/handoffs` | Create Handoff (from → to) |
| POST | `/v1/handoffs/{id}/complete` | Complete Handoff |
| GET/POST | `/v1/projects/{id}/decisions` | List / create Decision (options + recommendation) |
| POST | `/v1/decisions/{id}/answer` | Answer a Decision |
| GET | `/v1/projects/{id}/activity?type=` | Activity feed, filter by Type |
| POST | `/v1/tasks/{id}/runs` | Create Run |
| GET/PATCH | `/v1/runs/{id}` | Get / update Run status |

Auth is stubbed. Pass `X-Member-ID` or `member_id` when posting messages.

## Docker

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

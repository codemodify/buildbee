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
| GET/POST | `/v1/projects` | List / create Project (seeds owner + Scout/Builder/Sentry/Pulse and `#general`) |
| GET | `/v1/projects/{id}` | Project + Members + Channels |
| GET/POST | `/v1/projects/{id}/members` | List / add Member (`kind`: human \| bot) |
| GET/POST | `/v1/projects/{id}/channels` | List / create Channel |
| GET/POST | `/v1/channels/{id}/messages` | List / post Message |
| GET | `/v1/channels/{id}/ws` | WebSocket: pushes new Channel messages |
| GET/POST | `/v1/projects/{id}/tasks` | List / create Task (`?handoff=scout\|builder\|none`, default Scout) |
| GET/PATCH | `/v1/tasks/{id}` | Get / update status |
| POST | `/v1/tasks/{id}/handoffs` | Create Handoff (`to_role`, `?autorun=1` enqueues a Run for Builder) |
| POST | `/v1/handoffs/{id}/complete` | Complete Handoff (Scout + ambiguous Task → Decision stub) |
| GET/POST | `/v1/projects/{id}/decisions` | List / create Decision (options + recommendation) |
| POST | `/v1/decisions/{id}/answer` | Answer a Decision |
| GET | `/v1/projects/{id}/activity?type=` | Activity feed, filter by Type |
| POST | `/v1/tasks/{id}/runs` | Create Run |
| GET | `/v1/tasks/{id}/runs` | List Runs |
| GET | `/v1/tasks/{id}/detail` | Task + Runs + Artifacts + Pipelines |
| GET/PATCH | `/v1/runs/{id}` | Get / update Run status |
| GET/POST | `/v1/tasks/{id}/artifacts` | List / create Artifact (logs, PR URL, files) |
| POST | `/v1/tasks/{id}/pr` | Draft Repo PR (GitHub) or fake PR Artifact |
| GET/POST | `/v1/tasks/{id}/pipelines` | List / create Pipeline check |
| PATCH | `/v1/pipelines/{id}` | Update Pipeline status |
| POST | `/v1/pipelines/webhook` | Record Pipeline status (simple JSON or GitHub `check_run`) |
| GET | `/v1/auth/me` | Identity + auth mode (`dev` or OAuth) |
| GET | `/v1/auth/github` | Start GitHub OAuth (humans) |
| POST | `/v1/projects/{id}/issues/sync` | Upsert Tasks from GitHub Issues (`fake` without token) |
| POST | `/v1/issues/webhook` | GitHub Issues webhook → Task |
| GET/POST | `/v1/projects/{id}/routines` | List / create Routine |
| POST | `/v1/routines/{id}/run` | Force-fire a Routine |

Auth is stubbed. Pass `X-Member-ID` or `member_id` when posting messages.

## Docker

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

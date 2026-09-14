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
| PATCH | `/v1/projects/{id}` | Update `auto_run` |
| GET/POST | `/v1/projects/{id}/members` | List / add Member (`kind`: human \| bot) |
| GET/POST | `/v1/projects/{id}/channels` | List / create Channel |
| GET/POST | `/v1/channels/{id}/messages` | List / post Message |
| GET | `/v1/channels/{id}/ws` | WebSocket: pushes new Channel messages |
| GET/POST | `/v1/projects/{id}/tasks` | List / create Task (`?handoff=scout\|builder\|none`, default Scout) |
| GET/PATCH | `/v1/tasks/{id}` | Get / update status |
| POST | `/v1/tasks/{id}/handoffs` | Create Handoff (`to_role`, `?autorun=1` enqueues a Run for Builder) |
| POST | `/v1/handoffs/{id}/complete` | Complete Handoff (Scout + ambiguous Task → Decision stub) |
| GET/POST | `/v1/projects/{id}/decisions` | List / create Decision (`assignee_id`; `?inbox=1` unanswered; `?mine=1&member_id=` mine or unassigned) |
| GET | `/v1/projects/{id}/decisions/memories` | Remembered answers (don’t-ask-twice) |
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
| POST | `/v1/issues/webhook` | GitHub Issues webhook → Task (`GITHUB_WEBHOOK_SECRET` verifies `X-Hub-Signature-256`) |
| GET/POST | `/v1/projects/{id}/routines` | List / create Routine |
| POST | `/v1/routines/{id}/run` | Force-fire a Routine |
| GET | `/v1/notifications` | Inbox (`member_id` or `X-Member-ID`, `?unread=1`) |
| POST | `/v1/notifications/{id}/read` | Mark one Notification read |
| POST | `/v1/notifications/read-all` | Mark all read for `member_id` |
| GET/PATCH | `/v1/me/preferences` | Mute mentions / Routine digests (`member_id` or GitHub session) |
| GET/POST | `/v1/projects/{id}/invites` | List pending Invites / create (owner or admin; email and/or GitHub login, role `member`\|`admin`) |
| GET | `/v1/invites/{token}` | Preview Invite (public; token is the secret) |
| POST | `/v1/invites/{token}/accept` | Join Project (dev session or GitHub Identity) |
| DELETE | `/v1/invites/{id}` | Revoke (owner or admin) |

Auth is stubbed. Pass `X-Member-ID` or `member_id` when posting messages. Invite create/revoke require Role **owner** or **admin**; any human Member may list pending Invites.

Notifications are created when a Decision opens (not reused), a Handoff targets a Member, a Channel `@bot` mention creates a Task, or a Pipeline records `failure`.

## Web UI

The Server serves the Vite SPA for non-`/v1` / non-`/healthz` routes when `web/dist` is embedded (`go:embed` of `internal/webui/dist`) or `BUILDBEE_WEB_DIR` points at a built `dist`.

```bash
# from repo root
make run                 # build web, serve via BUILDBEE_WEB_DIR
make build               # embed dist, write bin/buildbee-server
./scripts/build.sh
```

## Docker

Prefer the **repo-root** `Dockerfile` (builds web, then Server). Compose uses that context so one service hosts API + UI.

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

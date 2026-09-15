# Changelog

## Unreleased — LAN rebuild, Phase 3a (Run queue)

### Breaking
- Workers pull Runs: `POST /v1/worker/claim` and `POST /v1/runs/{id}/heartbeat`. The worker's `POST /runs` endpoint, `BUILDBEE_WORKER_ADDR`, `BUILDBEE_FAKE_SANDBOX` and the command Sandbox are gone
- Only the claiming worker may write to a Run (`409` otherwise)
- `buildbee run start --task ID [--agent A] [--bot ID] [--follow]` queues on the Server; `--acp`, `--fake`, `--cmd`, `--repo-url` and `BUILDBEE_WORKER_URL` are gone
- New baseline schema: recreate the database

### Added
- Postgres Run queue with leases, heartbeats and a reaper; new Runs wake waiting workers; `buildbee run show|cancel`
- Runs carry `agent`, `prompt`, `worker`, `attempts`; Bots have an `agent` (`PATCH /v1/members/{id}`); Projects have `repo_url` and `default_branch`
- Workers: `BUILDBEE_WORKER_NAME`, `BUILDBEE_WORKER_AGENTS`, `BUILDBEE_WORKER_SLOTS`; [docs/workers.md](docs/workers.md)
- The compose smoke test runs a Run end to end through the compose worker

## LAN rebuild, Phase 1

### Breaking
- New baseline schema: recreate the database
- Identity: the acting Person comes from the `buildbee_person` cookie (`POST /v1/me`), `X-BuildBee-As` or `X-BuildBee-Worker`. `X-Member-ID`, `member_id` and `from_member_id` are gone
- Inbox and preferences are per Person: `/v1/me/notifications`, `/v1/me/preferences`
- Messages, Activity and notifications return `{items, has_more}` and page by `seq`; Artifact listings omit `body` (fetch `/v1/artifacts/{id}`)
- Unknown Task, Run and Pipeline statuses are rejected; illegal Run transitions, second Handoff completions and second Decision answers are `409`
- The CLI acts as `BUILDBEE_AS` (default: your login name); `handoff create` has no `--from`
- Workers refuse real agent CLIs unless `BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1`

### Added
- `internal/core`: every rule in one transaction with its Activity and notifications, events published after commit
- `GET /v1/ws` multi-topic WebSocket with cursor replay; slow clients are disconnected instead of blocking the Server
- People and `@Person` mentions; Project and Channel archive; Task descriptions; Handoff listing; Decision `task_id`; Run `bot_member_id`, `started_at`, `finished_at`
- Routine enable/reschedule (`PATCH /v1/routines/{id}`); Routines fire once even with concurrent schedulers
- [API reference](docs/api.md)

## Unreleased — LAN rebuild, Phase 0

BuildBee now targets one Server on a trusted LAN where people and agents work on many Projects. See [ADR 0002](docs/adr/0002-lan-agent-harness.md) and the [roadmap](docs/roadmap.md).

### Breaking
- No login: GitHub OAuth, sessions, Invites and GitHub-linked identities are gone. Members have no `identity`, `github_login` or `github_id`; preferences are per Member
- No cross-origin access; the desktop shell is parked and the Server URL setting is gone
- Postgres is required (`DATABASE_URL`); there is no in-memory store
- One baseline schema: databases created by earlier builds must be recreated
- `BUILDBEE_RUNTIME_*` is now `BUILDBEE_WORKER_*`; Railway configs and the `PORT` fallback are removed
- `/pr` and `/issues/sync` return 503 without `GITHUB_TOKEN` / `GITHUB_REPO` instead of fake results; fakes require `{"fake":true}`

### Changed
- One Go module: `cmd/buildbee-server`, `cmd/buildbee-worker`, `cmd/buildbee`
- Validated configuration; timeouts, body limit and graceful shutdown; request log; 5xx causes logged and hidden from clients; `/healthz` checks Postgres
- Migrations apply under an advisory lock, one transaction per file, with checksums
- Compose: Postgres unpublished, worker bound to loopback, health checks and restart policies
- The CLI marks a Run failed when the worker errors instead of recording a fake success; the worker fails on a missing agent or Docker instead of faking
- CI runs Go tests on Postgres, a gofmt check, the web build, and a compose smoke test

### Fixed
- Agent output still in the pipe when the agent exited (often its final summary) was lost
- A single output line over 1 MiB stalled the agent until the 3-minute kill
- NUL bytes in agent output broke RunEvents and log Artifacts on Postgres
- @mention Task titles split UTF-8 characters and were silently dropped on Postgres
- Decisions answered from memory never showed as reused in listings
- Concurrent Issue syncs could create duplicate Tasks
- Deleting a used Project failed on member foreign keys
- Stale asset requests after an upgrade got index.html instead of a 404

## v0 — feature complete

BuildBee v0 on `dev` (PR #1). Apache-2.0.

A **Project** workspace where humans and **Bots** cooperate. One Server process can host `/v1`, `/healthz`, and the Vite SPA.

### Workspace
- Project, Channel (REST + WebSocket), Task, Handoff, Decision (+ fingerprint memory), Activity
- Seeded Bot Roles: Scout, Builder, Sentry, Pulse
- Member Invite (token link; owner/admin create/revoke; members list)
- Cross-Project human **Identity**: same GitHub login reuses one profile on every Invite accept

### Execution
- Run + Artifact + Pipelines webhook; FakeACP or Docker Sandbox
- CLI `buildbee run start --task --acp [--agent fake]`
- Issues→Task sync; Routines (`morning-digest`)

### Identity and inbox
- Dev auth (default) or GitHub OAuth
- Notifications: Decision (assignee or all humans), Handoff, @mention Task, Pipeline failure, Invite accept, Routine fire
- `GET/PATCH /v1/me/preferences` — mute mentions / Routine digests
- Decision `assignee_id` + `?mine=1` inbox

### Web
- Same-origin `/v1` (Vite `base: "/"`); hash routes including `#/invite/:token`
- Kanban Tasks, Decisions (assignee + mine), Invites, Activity, inbox bell, mute prefs
- Empty states and inline errors/toasts on Invite, Sync Issues, Routine run, Start Run
- Persistent Projects rail + two side-by-side Project panes (Alt-click or ⊕ opens beside). Header Project `<select>` removed. `#/projects/:id?beside=` + localStorage restore
- Slack-style Projects tree: collapsible Project nodes, nested `#channel` navigator, Channel create under each Project; in-pane Channel list removed
- Light theme by default (`html[data-theme=light]` + `--bb-*` tokens). Amber accents kept. No theme switcher in v0.

### Ship
- Compose (Postgres 16 + Server) and Railway (`Dockerfile`, `railway.toml`)
- `make build` / `scripts/build.sh` embed `web/dist`
- Desktop MVP (`desktop/`): Tauri 2 window loads `web/`; Server URL persists locally; native Reload / Open Server URL / Quit. Server (and Postgres) are not bundled.
- Live ACP streaming: RunEvent (`token` / `tool_call` / `tool_result` / `status` / `log`), `GET /v1/runs/{id}/events?after=`, WebSocket `/v1/runs/{id}/ws`. FakeACP emits chunks over ~1–2s. Final Artifact remains `acp.log`. Web Task page shows a live transcript.
- CI: Go tests, web build, desktop `cargo check` on Ubuntu (no signed `.dmg`), e2e scripts against in-memory Server (no DinD) including `e2e-acp-stream.sh`

### Pinned later
IDE extension, Nostr/NIP-01 signed Activity, production GitHub App install UI.

### Deferred
Desktop tray / multi-window bench.

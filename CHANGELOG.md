# Changelog

## Unreleased — Phase 2a (chat backend)

### Added
- Threads (`GET /v1/messages/{id}/thread`, `POST /v1/messages/{id}/replies`); Channels list root messages with `reply_count`
- Every Task has a thread (`thread_id`) where its Bots report; replies there steer the working agent; `@Bot` there hands the same Task on
- DMs (`/v1/projects/{id}/dms`); a DM with a Bot asks it to work
- Unread counts and read markers; `GET /v1/presence` and the `presence:server` topic

### Changed
- Mentioning a Bot starts its Run at once (before: only with autopilot)
- Handing a Task to Sentry before anything was pushed starts no review
- Events of one topic are published in cursor order

## Phase 4d (usage, cleanup)

### Added
- Runs keep `context_tokens`, `context_size`, `cost` and `cost_currency` from agents' usage reports; `GET /v1/usage`, `GET /v1/projects/{id}/usage`, `buildbee usage`

### Removed
- `POST /v1/tasks/{id}/pr` (single-file draft PRs from the Server). Workers open and merge PRs with their own `gh` login

### Changed
- Roadmap: desktop (Linux, Windows, macOS) and mobile (Android, iOS) apps as Phase 6, one Tauri 2 app on the web UI

## Fixes from the adversarial review

### Security
- An agent could get code run on the worker host through git metadata (hooks, `core.fsmonitor`, a redirected `.git`) in the mirror mounted into its container. Agents now work in a repo of their own that borrows the mirror's objects read-only; the worker commits their files from a worktree only it uses, and runs git with hooks and fsmonitor off
- Agent logins are copied into the Run's home instead of mounting the host's agent directories; only refreshed tokens are written back

### Fixed
- Autopilot merges exact commits: Sentry's approval and CI must be for the Task's head commit; merge Runs merge that commit (`--match-head-commit`, or fail if the branch moved); merge Decisions name their commit
- CI results arriving together are judged one after another (Task row lock), so merges no longer stall or queue twice
- CI results are matched to pushes by commit (`head_sha`), including results that arrive before the build reports
- A long failure message no longer makes the worker's final report fail; reports are retried
- A briefly unreachable Server drops a few events instead of killing the Run
- "Merge anyway?" is asked once; Runs failed by the reaper notify people; `max_runs` holds when claims race; canceled Tasks never merge; merges never delete a branch someone pushed to after the merge started

## LAN rebuild, Phase 4c (Routines)

### Changed
- Routines take a `prompt`: each firing opens a Task with it, handed to Scout (or `bot_member_id`), which starts a Run; a Routine skips while its last Task is open
- New Projects no longer get a disabled `morning-digest` Routine
- `buildbee routine create`

## LAN rebuild, Phase 4b (steering, fair scheduling)

### Added
- `POST /v1/runs/{id}/steer` and `buildbee run steer`: message the agent working on a Run, queued or with `interrupt`
- Project `max_runs`; claims serve the Project with the fewest Runs going first
- RunEvent kind `steer`; recreate the database

## LAN rebuild, Phase 4a (autopilot)

### Added
- [Autopilot](docs/autopilot.md): with `auto_run`, Scout plans, Builder builds and pushes, Sentry reviews, and approved branches merge after CI; changes and CI failures go back to the Builder; after 3 builds a person decides
- Run `kind` (`plan`, `build`, `review`, `merge`), `summary`, `branch`, `pr_url`, `verdict`; Task `branch`, `pr_url`, `merged_at`; Project `merge_policy` and `instructions`; Decision `action`
- CI webhooks match Tasks by branch; merge Runs run on any worker (`gh pr merge` or `git merge`)

### Changed
- Builds on a Task continue its branch; pushes use `--force-with-lease`
- Handing a Task to Scout or Sentry starts a Run when autorun or autopilot is on (before: only the Builder)
- Requests the client abandoned are no longer logged as server errors

## LAN rebuild, Phase 3b (per-Run containers)

### Breaking
- Workers run each agent in its own container by default; `BUILDBEE_WORKER_ALLOW_HOST_AGENTS` is replaced by `BUILDBEE_WORKER_ISOLATION=host`

### Added
- `Dockerfile.agents` (claude-agent-acp, codex-acp, OpenCode, Grok); `BUILDBEE_WORKER_IMAGE`, `_MEMORY`, `_CPUS`, `_NETWORK`
- Workers probe the image for agents and remove containers left by a previous run
- `buildbee-worker fake-agent` speaks ACP on stdio, for trying container isolation without a real agent
- ACP `authenticate` is called when an agent asks for it, reusing the CLI's login

## LAN rebuild, Phase 3d (repos and pull requests)

### Added
- Runs on a Project with `repo_url` work in their own git worktree on branch `buildbee/<task>-<run>`; the worker commits, uploads `changes.diff`, pushes with its own git credentials and opens a PR with `gh`
- `BUILDBEE_WORKER_DIR`, `BUILDBEE_WORKER_OPEN_PRS`

### Security
- Repo URLs starting with `-` or using remote helpers (`ext::`, `fd::`) are refused; workers run git without prompts over file, git, http(s) and ssh only

## LAN rebuild, Phase 3c (ACP client)

### Breaking
- Workers run agents over the Agent Client Protocol instead of one-shot CLI calls: Claude Code needs `claude-agent-acp`, Codex needs `codex-acp` (see [docs/workers.md](docs/workers.md))
- New RunEvent kinds `thought`, `plan` and `usage`; recreate the database

### Added
- `BUILDBEE_AGENT_<NAME>` launch overrides and `BUILDBEE_WORKER_RUN_TIMEOUT`
- Unattended sessions (permission-skipping mode or logged one-time approvals), cancel with process-group kill, login-required errors that say what to do

## LAN rebuild, Phase 3a (Run queue)

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

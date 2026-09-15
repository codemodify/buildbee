# Roadmap

BuildBee is being rebuilt into a LAN harness where people and coding agents work on many Projects in parallel. Decisions are recorded in [ADR 0002](adr/0002-lan-agent-harness.md).

## Phase 0 — Cleanup (done)

- One Go module; `cmd/buildbee-server`, `cmd/buildbee-worker`, `cmd/buildbee`
- Postgres is the only store; tests run on real Postgres; CI runs a Postgres service
- Login, sessions, Invites, CORS and Railway removed; cross-site writes refused
- One baseline schema; migrations locked, atomic and checksummed
- Validated config, timeouts, graceful shutdown, request logs, 5xx causes logged, database-backed health check
- No fake successes: missing agents, workers or GitHub configuration are errors
- Postgres-only bugs fixed: NUL bytes in agent output, mid-character title truncation, Decision memory fields, duplicate issue Tasks, Project deletion
- Bash e2e scripts replaced by Go tests and a compose smoke test

## Phase 1 — Core platform (done)

- `internal/core` holds every rule; each operation is one transaction that writes the change, its Activity and its notifications, and publishes events only after commit. HTTP, webhooks and the Routines scheduler all go through it
- People: you pick a name once; the Server resolves the acting Person from a cookie, `X-BuildBee-As` or `X-BuildBee-Worker`. No client-asserted Member IDs, no "first human" fallback; writing to a Project joins it
- Activity is an event log with the actor on every entry
- WebSocket hub: one socket for many topics, replay by cursor with no gap or duplicate, per-connection queues and write deadlines, slow clients dropped instead of freezing the Server
- Cursor paging for messages, Activity and notifications; Artifact listings without bodies
- Status vocabularies and transitions enforced in code and in the schema; Handoffs complete once, Decisions answer once, finished Runs take no events
- Project and Channel archive; Handoffs readable; Task descriptions; `@Person` mentions notify
- Routines claim each firing, so concurrent schedulers fire once
- Interim guard: workers refuse real agent CLIs on the host unless `BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1`, and then give each Run an empty temporary directory

## Phase 3a — Run queue (done)

- Runs are a Postgres queue: workers long-poll `POST /v1/worker/claim`, one worker gets each Run (`FOR UPDATE SKIP LOCKED`), new Runs wake waiting workers at once
- 60 s leases renewed by heartbeats; a canceled Run stops within one heartbeat; the Server fails Runs whose worker went silent (no blind retries)
- Only the claiming worker writes to a Run; each Run carries its agent and prompt; Bots have an `agent`; Projects have `repo_url` and `default_branch`
- Workers open no port and run N slots; the CLI queues Runs and `--follow`s them; the push-style worker endpoint and the command Sandbox are gone

## Phase 3c — ACP client (done)

- Agents run over the Agent Client Protocol: `claude-agent-acp`, `codex-acp`, `grok agent stdio`, `opencode acp`, `goose acp`, each using its CLI's own login on the worker
- Replies, reasoning, plans, tool calls and results and usage stream as RunEvents, merged so a Run posts a few events per second
- Unattended sessions: permission-skipping mode when offered, otherwise one-time approvals, each logged; login-required errors say what to do
- Cancel sends `session/cancel`, then kills the agent's process group; Runs have a time limit
- The fake agent is an in-process ACP agent, so tests exercise the same client code as real agents

## Phase 3d — Repos and pull requests (done)

- Each Run gets a git worktree of the Project's repo on its own branch, from a per-repo bare mirror on the worker; parallel Runs never share a checkout
- The worker commits leftovers, uploads `changes.diff`, pushes with its own git credentials and opens a PR with its own `gh` login; a failed push keeps the branch on the worker
- Repo URLs that git could read as options or remote helpers are refused

## Phase 3b — Per-Run containers (done)

- Default isolation: each Run's agent in its own container (`docker run -i` carrying ACP), as the worker's user, capabilities dropped, resource limits, no Docker socket, removed on finish, cancel or worker restart
- Only the Run's checkout, its mirror and that agent's login are mounted
- `Dockerfile.agents` with `claude-agent-acp`, `codex-acp`, OpenCode and Grok; workers probe the image for agents
- Host isolation stays as the Buzz-style opt-in (`BUILDBEE_WORKER_ISOLATION=host`)
- Agents that want an explicit ACP `authenticate` (Grok) get one, reusing their CLI's login

## Phase 4a — Autopilot (done)

- Runs have kinds: `plan` (Scout), `build` (Builder), `review` (Sentry, with a parsed verdict) and `merge` (no agent, any worker)
- With `auto_run`, each finished Run moves its Task on: plan → build → review → merge; changes requested or failed CI go back to the Builder on the same branch; after 3 builds a person decides
- CI gate on the latest push (GitHub `check_run` matched by branch); `merge_policy: approval` asks with a `merge` Decision
- Builds continue the Task's branch and push with a lease; merges use `gh pr merge` or `git merge`
- Project `instructions` reach every agent; Runs report the agent's closing message as `summary`

## Phase 2a — Chat backend (done)

- Threads: replies under a root message; Channels list roots with reply counts
- Every Task has a thread: Bots post there when they start, finish, fail or need a Decision; a person's reply reaches the agent at work, and `@Bot` in it hands the Task on
- A mention or a DM to a Bot opens a Task and starts its Run
- DMs between chosen Members; unread counts from per-person read markers; presence of people (open sockets) and workers (claims and heartbeats)

## Phase 4d — Usage and cleanup (done)

- Each Run keeps its peak context and the session cost agents report; `GET /v1/usage` and `/v1/projects/{id}/usage` sum them by agent and Project (`buildbee usage`)
- The Server-side single-file draft PR endpoint is gone: workers open and merge PRs with their own `gh` login; the Server's GitHub token only syncs Issues

## Phase 4c — Routines that do work (done)

- A Routine has a prompt; each firing opens a Task with it and hands it to Scout (or a chosen Bot), which starts a Run
- A Routine skips while the Task it opened last is still open; Projects no longer get a do-nothing Routine seeded

## Phase 4b — Steering and fair scheduling (done)

- People message a working agent: queued Runs get it in their prompt; running agents get it as their next turn over ACP, or at once with `interrupt` (the turn is canceled first)
- Workers follow each Run's WebSocket from the claim's cursor, so messages arrive live and never twice
- Per-Project `max_runs`, and claims serve the Project with the fewest Runs going first

## Phase 2b — Chat client (done)

- One WebSocket feeds the whole client, resubscribing from the last cursor after a reconnect; no polling
- Projects, Channels and DMs with unread badges and presence; threads beside the channel (full screen on phones); `@` autocomplete
- A Task's thread shows its state and its live Run: the agent's reply as it streams, plan, tool calls, reasoning and log, with messages to the agent, interrupt and cancel
- Board, Task page (Runs, diff, transcripts, CI, Handoffs), Decisions, settings (autopilot, merge policy, parallel Runs, repo, instructions, each Bot's agent, Routines), usage, notifications, workers online
- Light and dark themes; phone-sized layouts, ready for the Phase 6 apps

## Phase 2 — Chat UX, remaining

- Message search; editing and deleting messages; file and image attachments
- Keyboard navigation between Channels; desktop notifications from the browser

## Phase 3 — Agent harness, remaining

- Per-Project images
- Permission requests as Decisions for Projects that want them, answers flowing back to the agent; mid-turn steering from chat
- Logs and large Artifacts in object storage

## Phase 4 — Autonomy, remaining

- Steering from chat threads (the API and CLI exist; the UI comes with Phase 2)

## Phase 5 — Scale and operations

- Dashboard across all Projects and Runs, metrics, retention, backups, load test with dozens of concurrent Runs

## Phase 6 — Desktop and mobile apps

One app for every platform, built on the Phase 2 web UI, so every client gets the same features at once:

- Tauri 2 app from `desktop/`: Linux (AppImage, .deb, .rpm), Windows (MSI), macOS (universal .dmg), Android (APK/AAB) and iOS, wrapping `web/` and talking to a Server on the LAN
- First run asks for the Server's address (or finds it with mDNS on the LAN); several Servers can be saved
- Native notifications for mentions, Decisions to answer and failed Runs; a tray/menu-bar badge on desktop
- The web UI is also an installable PWA, for phones and machines without the app
- CI builds and signs the packages; releases carry checksums
- Needs, before shipping outside a trusted LAN: authentication (ADR 0002 keeps it out for now), TLS to the Server

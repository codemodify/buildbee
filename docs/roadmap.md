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

## Phase 2 — Chat UX

- Channels, threads, direct messages, @people and @agents, unread counts, presence
- Agent output streamed live into threads
- A client state layer fed by the single WebSocket, replacing polling loops

## Phase 3 — Agent harness

- Per-Run container: repo worktree at a pinned commit, agent CLI inside, only the credentials that Run needs, no Docker socket, resource and output limits, process-group kill
- ACP over stdio for `claude-agent-acp`, `goose acp` and `codex-acp`: tool events, permission requests become Decisions, answers flow back to the agent, mid-turn steering
- Output: pushed branch, diff Artifact and PR; logs in object storage

## Phase 4 — Autonomy

- Per-Project orchestrator: backlog → Scout triage → Builder → Sentry review → PR → CI feedback
- Configurable Bots (agent, model, prompt, tools) and a Project autonomy policy: auto-merge by default, human approval optional
- Heartbeat prompts, agent memory, scheduled Routines, token and cost tracking

## Phase 5 — Scale and operations

- Dashboard across all Projects and Runs, metrics, retention, backups, load test with dozens of concurrent Runs

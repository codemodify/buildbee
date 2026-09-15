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

## Phase 1 — Core platform

- Domain service layer: every rule (mentions, handoffs, decisions, runs) in one place, used by HTTP, workers and Routines alike
- One Actor per request: a chosen name resolved server-side; no more `X-Member-ID` / `member_id` trust or "first human" fallbacks
- Activity as an event log: typed, with an actor, written in the same transaction as the change
- WebSocket hub: one connection per client, topic subscriptions, replay by cursor, per-connection queues with write deadlines
- Cursor pagination, enforced state machines, Project and Channel archive

## Phase 2 — Chat UX

- Channels, threads, direct messages, @people and @agents, unread counts, presence
- Agent output streamed live into threads
- A client state layer fed by the single WebSocket, replacing polling loops

## Phase 3 — Agent harness

- Postgres job queue with claims, leases, heartbeats, retries and a reaper; workers pull Runs
- Per-Run container: repo worktree at a pinned commit, agent CLI inside, only the credentials that Run needs, no Docker socket, resource and output limits, process-group kill
- ACP over stdio for `claude-agent-acp`, `goose acp` and `codex-acp`: tool events, permission requests become Decisions, answers flow back to the agent, mid-turn steering
- Output: pushed branch, diff Artifact and PR; logs in object storage

## Phase 4 — Autonomy

- Per-Project orchestrator: backlog → Scout triage → Builder → Sentry review → PR → CI feedback
- Configurable Bots (agent, model, prompt, tools) and a Project autonomy policy: auto-merge by default, human approval optional
- Heartbeat prompts, agent memory, scheduled Routines, token and cost tracking

## Phase 5 — Scale and operations

- Dashboard across all Projects and Runs, metrics, retention, backups, load test with dozens of concurrent Runs

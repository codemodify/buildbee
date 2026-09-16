# ADR 0002: LAN agent harness

- Status: Accepted
- Date: 2026-09-15
- Supersedes: the Auth, Integrations and Desktop rows of [ADR 0001](0001-stack.md)

## Context

BuildBee v0 was a scaffold that aimed at a hosted, multi-user product: GitHub OAuth, Invites, a desktop app talking to remote Servers, Railway deploys, and an in-memory store for tests. An adversarial architecture review found that the design fit none of those goals well, and that CI never exercised the Postgres path the product actually runs.

The owner's goal is different: a Slack/Discord-style workspace where people and coding agents work on many Projects in parallel and autonomously, on a secure LAN, with no authentication. Confirmed 2026-09-16: BuildBee is for secure LANs only. It is not made safe to expose to the internet, so authentication and TLS stay out of the plan; the security effort goes into what agents may do (containers, the bubblewrap sandbox, permission Decisions) and into keeping browsers from being used against the Server (same-origin writes, sandboxed downloads).

## Decisions

| Area | Decision |
| --- | --- |
| **Deployment** | One Server per company or individual, on a trusted LAN, hosting many Projects. Workers run on any LAN machine with Docker. Target: 20–50 concurrent Runs. |
| **Auth** | None. GitHub OAuth, sessions and Invites are removed. A lightweight "who is acting" identity (a chosen name) will be resolved in one place so real auth can slot in later. |
| **Browser safety** | With no login, cross-site state-changing requests are refused and WebSockets are same-origin only. |
| **Store** | Postgres only. The in-memory store is deleted; tests run against Postgres. |
| **Schema** | One baseline migration, edited in place until the first release. The runner refuses databases with unknown or edited migrations. |
| **Code layout** | One Go module: `cmd/buildbee-server`, `cmd/buildbee-worker`, `cmd/buildbee`, shared `internal/` packages. |
| **Agents** | Follow Block's Buzz: each agent is a chat member driven by an ACP harness with N parallel sessions, mid-turn steering, turn limits, thread context, heartbeats and memory. Agent credentials live on the worker machine, never in BuildBee. Unlike Buzz, every Run executes in a per-Run container. |
| **Agents first** | `claude-agent-acp`, `goose acp` and `codex-acp`; OpenCode after. |
| **Repos** | GitHub, with a per-Project repo and token. |
| **Autonomy** | Agents commit and, by default, merge when CI is green and the Sentry review agent approves. Each Project can require human approval instead. |
| **Apps** | The browser on the LAN is the first client. Desktop (Linux, Windows, macOS) and mobile (Android, iOS) apps follow the chat UI: one Tauri 2 app wrapping `web/`, pointed at a Server on the LAN, plus the web UI installable as a PWA. (Updated 2026-09-15: was "Desktop parked".) |
| **Fakes** | FakeACP, the fake Sandbox, fake PRs and sample Issues run only when explicitly requested. Missing configuration is an error, never a fake success. |

## Consequences

- Anyone on the LAN can read and change every Project. That is acceptable for the target deployment and must be revisited before BuildBee is exposed beyond a trusted network.
- A pre-release database must be recreated when the baseline schema changes.
- Workers pull Runs from a Postgres-backed queue with leases and heartbeats; they open no port (Phase 3a, [workers.md](../workers.md)).

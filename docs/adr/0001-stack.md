# ADR 0001: v0 stack

- Status: Accepted; Auth, Integrations and Desktop superseded by [ADR 0002](0002-lan-agent-harness.md)
- Date: 2026-09-13

## Decision

BuildBee v0 uses this stack:

| Layer | Choice |
| --- | --- |
| **Server** | Go + Postgres. Redis is optional later (presence, pub/sub). REST + WebSocket. Docker for local and deploy. |
| **Web** | React + Vite + TypeScript + Tailwind. |
| **CLI** | Go binary `buildbee`. |
| **Runtime** | Go supervisor + Docker **Sandbox** for ACP agent **Runs**. |
| **Auth** | Stub in this scaffold. Humans: GitHub OAuth later. Bots: server-issued **Identities**. |
| **Integrations** | GitHub later as **Repo**, **Issues**, and **Pipelines**. |
| **License** | Apache-2.0. |
| **Desktop** | Tauri 2 wrapping `web/`. Talks to a local or remote Server. Does not bundle Postgres. |

## Why these defaults

- One language (Go) for Server, CLI, and runtime keeps the monorepo small.
- Postgres is enough for Project, Channel, Task, Decision, Artifact, and Activity records.
- Docker Sandboxes isolate Bot Runs from the host.
- React + Vite is a fast web skeleton without inventing a design system.
- GitHub OAuth matches the later Repo / Issues / Pipelines integration.

## Activity signing (later)

Activity may later be signed in a Nostr-inspired way so Members can verify who did what. This ADR does **not** adopt NIP-01 or a Nostr relay. Do not implement a full event store in the scaffold.

## Consequences

- Go modules live under `github.com/codemodify/buildbee/{server,cli,runtime}` with a root `go.work`.
- The web app is a separate Node package in `web/`.
- The desktop app is a Tauri 2 host in `desktop/` that embeds `web/dist` and calls the Server over HTTP.
- `deploy/compose` runs Postgres and the Server image. MinIO is an optional profile for later Artifact object storage.

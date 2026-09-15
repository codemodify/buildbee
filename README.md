# BuildBee

BuildBee is a chat workspace where people and coding agents work on **Projects** together. People and **Bots** share Channels, hand Tasks to each other, record Decisions, and Bots execute **Runs** that stream their work back into the Project.

It runs as one Server on a trusted LAN. There is no login: open the Server in a browser and start working. Many Projects share one Server, and Runs execute on any number of worker machines that pull them from the Server.

The rebuild toward massively parallel, autonomous agent work is in progress. See the [roadmap](docs/roadmap.md) for what exists today and what comes next, and [ADR 0002](docs/adr/0002-lan-agent-harness.md) for the decisions behind it. Product nouns are fixed in the [glossary](docs/glossary.md); the [API reference](docs/api.md) covers identity, endpoints, paging and live events.

License: [Apache-2.0](LICENSE).

## Run it

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
# http://<this-host>:8080
```

That starts Postgres and the Server with the web UI. Add a demo worker (fake agent only):

```bash
docker compose -f deploy/compose/docker-compose.yml --profile worker up --build
```

Real agents run on machines where their CLIs are logged in; see [docs/workers.md](docs/workers.md). Turn on [autopilot](docs/autopilot.md) and a Project's Bots take Tasks from plan to merged code by themselves. In the web app, ask for work by mentioning a Bot (`@Builder add rate limiting`) and follow it in the Task's thread, live. [deploy/README.md](deploy/README.md) covers configuration, backups and upgrades.

## Develop

Requirements: Go 1.27, Node 24, Docker (for Postgres in tests and the compose stack).

```bash
make test        # gofmt check, go vet, Go tests on Postgres, web build
make run         # Server with the web UI served from web/dist (needs DATABASE_URL)
make build       # bin/buildbee-server (UI embedded), bin/buildbee-worker, bin/buildbee
make smoke       # build both images and exercise the compose stack
```

Go tests run against a real Postgres. With `BUILDBEE_TEST_DATABASE_URL` set they use that server; otherwise each test package starts a throwaway `postgres:17-alpine` container. Every test gets its own database cloned from a migrated template.

Schema changes ship as new migrations in `internal/migrate/sql` (`0002_...sql`, and so on). Released migrations are frozen: `internal/migrate/frozen.go` pins each file's checksum, a test fails if one changes, and the Server refuses a database whose applied migrations differ from the files. Upgrading applies the new files in order at startup, in one transaction each.

For UI work, run the Server and then `cd web && npm run dev`. Vite proxies `/v1` and `/healthz` (WebSocket included) to `127.0.0.1:8080`.

## Layout

| Path | What it is |
| --- | --- |
| [`cmd/buildbee-server`](cmd/buildbee-server) | Server: REST `/v1`, WebSockets, Routines worker, embedded web UI |
| [`cmd/buildbee-worker`](cmd/buildbee-worker) | Worker: claims queued Runs and executes them with agent CLIs |
| [`cmd/buildbee`](cmd/buildbee) | CLI for Projects, Tasks, Handoffs, Runs and Routines |
| [`cmd/buildbee-loadtest`](cmd/buildbee-loadtest) | drives a Server with many Runs at once and reports how it kept up |
| [`internal/core`](internal/core) | Business rules: one transaction per operation with its Activity and notifications |
| [`internal/httpapi`](internal/httpapi) | Thin HTTP handlers, identity resolution, middleware |
| [`internal/ws`](internal/ws) | WebSocket hub with cursor replay |
| [`internal/store`](internal/store) | Postgres persistence; [`internal/migrate`](internal/migrate) holds the schema |
| [`internal/worker`](internal/worker) | claim loop, heartbeats and reporting; [`acp`](internal/worker/acp) Agent Client Protocol client |
| [`internal/config`](internal/config) | Validated configuration for each binary |
| [`internal/testdb`](internal/testdb) | Per-test Postgres databases |
| [`web/`](web/) | Vite + React + TypeScript + Tailwind UI |
| [`deploy/compose`](deploy/compose) | Postgres + Server (+ worker) for one LAN host |
| [`desktop/`](desktop/) | Tauri shell; becomes the desktop and mobile apps ([roadmap](docs/roadmap.md) Phase 6) |
| [`docs/`](docs/) | Glossary, workflow, roadmap, ADRs |

## Configuration

| Variable | Binary | Default | Notes |
| --- | --- | --- | --- |
| `DATABASE_URL` | server | required | `postgres://…`; migrations run at startup |
| `BUILDBEE_ADDR` | server | `:8080` | listen address |
| `BUILDBEE_WEB_DIR` | server | embedded UI | serve the UI from a directory instead |
| `BUILDBEE_MAX_BODY_BYTES` | server | 32 MiB | request body limit |
| `BUILDBEE_BLOB_DIR` | server | `~/.local/share/buildbee/blobs` | attachments and large Artifacts ([deploy](deploy/README.md#files)) |
| `BUILDBEE_S3_ENDPOINT`, `_BUCKET`, `_REGION`, `_ACCESS_KEY`, `_SECRET_KEY` | server | unset | keep them in an S3-compatible bucket instead |
| `BUILDBEE_MAX_UPLOAD_BYTES` | server | 25 MiB | one attachment |
| `BUILDBEE_RETENTION_DAYS` | server | `90` | keep finished Runs' event streams and read notifications this long (0 = forever) |
| `BUILDBEE_LOCAL_WORKER` | server | `auto` | run agents on the Server's machine: `auto`, `on` or `off` ([workers.md](docs/workers.md)) |
| `GITHUB_TOKEN`, `GITHUB_REPO` | server | unset | Issue sync; without them it answers 503. Workers open PRs with their own `gh` login |
| `GITHUB_WEBHOOK_SECRET` | server | unset | require `X-Hub-Signature-256` on webhooks |
| `BUILDBEE_URL` | worker, CLI | `http://127.0.0.1:8080` | the Server |
| `BUILDBEE_AS` | CLI | your login name | the Person the CLI acts as |
| `BUILDBEE_WORKER_NAME` | worker | hostname | unique worker name; Runs are owned by it |
| `BUILDBEE_WORKER_AGENTS` | worker | detected | agents to offer, comma-separated ([workers.md](docs/workers.md)) |
| `BUILDBEE_WORKER_SLOTS` | worker | `4` | Runs executed at once |
| `BUILDBEE_WORKER_ISOLATION` | worker | `container` | `host` runs agents directly on the worker machine (see below) |
| `BUILDBEE_WORKER_SANDBOX` | worker | `auto` | host agents under bubblewrap: `auto`, `require` or `off` |
| `BUILDBEE_WORKER_IMAGE` | worker | `buildbee-agents` | agents image, built from `Dockerfile.agents` |
| `BUILDBEE_WORKER_RUN_TIMEOUT` | worker | `2h` | Run time limit |
| `BUILDBEE_WORKER_DIR` | worker | `~/.cache/buildbee-worker` | repo mirrors and Run checkouts |
| `BUILDBEE_WORKER_OPEN_PRS` | worker | `1` | open a PR with `gh` after pushing |
| `BUILDBEE_AGENT_<NAME>` | worker | built in | command that starts an agent over ACP |
| `BUILDBEE_TEST_DATABASE_URL` | tests | throwaway container | Postgres for `go test` |

Each binary validates its configuration at startup and refuses to start on bad input.

## Security model

BuildBee trusts the network it runs on. Anyone who can reach the Server can read and change every Project, and a Person is whoever claims their name: names attribute work, they do not prove identity. Protections that remain:

- State-changing requests from another site's page are refused, and WebSockets only accept same-origin pages, so a website a LAN user visits cannot drive the Server through their browser.
- Only the Server port is published by compose. Postgres stays on the compose network, and workers open no port.
- Only the worker that claimed a Run can write to it.
- By default each Run's agent runs in its own container that sees only the Run's checkout and that agent's login, with no Docker socket. `BUILDBEE_WORKER_ISOLATION=host` runs agents directly on the worker machine instead: sandboxed by bubblewrap where it is installed (the home hidden, only the checkout writable), otherwise with the worker user's files and logins.
- A Project can make agents ask before acting (`agent_permissions: ask`): each request waits for a person's answer in `# decisions`.
- Attachments are served so they cannot run as pages of the Server: only raster images inline, everything else as a sandboxed download.

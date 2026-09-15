# BuildBee

BuildBee is a chat workspace where people and coding agents work on **Projects** together. People and **Bots** share Channels, hand Tasks to each other, record Decisions, and Bots execute **Runs** that stream their work back into the Project.

It runs as one Server on a trusted LAN. There is no login: open the Server in a browser and start working. Many Projects share one Server, and Runs execute on worker machines next to Docker.

The rebuild toward massively parallel, autonomous agent work is in progress. See the [roadmap](docs/roadmap.md) for what exists today and what comes next, and [ADR 0002](docs/adr/0002-lan-agent-harness.md) for the decisions behind it. Product nouns are fixed in the [glossary](docs/glossary.md).

License: [Apache-2.0](LICENSE).

## Run it

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
# http://<this-host>:8080
```

That starts Postgres and the Server with the web UI. Add a worker on a machine that should execute Runs:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile worker up --build
```

[deploy/README.md](deploy/README.md) covers configuration, worker machines, backups and upgrades.

## Develop

Requirements: Go 1.27, Node 24, Docker (for Postgres in tests and for the Sandbox).

```bash
make test        # gofmt check, go vet, Go tests on Postgres, web build
make run         # Server with the web UI served from web/dist (needs DATABASE_URL)
make build       # bin/buildbee-server (UI embedded), bin/buildbee-worker, bin/buildbee
make smoke       # build both images and exercise the compose stack
```

Go tests run against a real Postgres. With `BUILDBEE_TEST_DATABASE_URL` set they use that server; otherwise each test package starts a throwaway `postgres:17-alpine` container. Every test gets its own database cloned from a migrated template.

For UI work, run the Server and then `cd web && npm run dev`. Vite proxies `/v1` and `/healthz` (WebSocket included) to `127.0.0.1:8080`.

## Layout

| Path | What it is |
| --- | --- |
| [`cmd/buildbee-server`](cmd/buildbee-server) | Server: REST `/v1`, WebSockets, Routines worker, embedded web UI |
| [`cmd/buildbee-worker`](cmd/buildbee-worker) | Worker: executes Runs in a Docker Sandbox or through an ACP agent |
| [`cmd/buildbee`](cmd/buildbee) | CLI for Projects, Tasks, Handoffs, Runs and Routines |
| [`internal/httpapi`](internal/httpapi) | HTTP handlers and middleware |
| [`internal/store`](internal/store) | Postgres store; [`internal/migrate`](internal/migrate) holds the schema |
| [`internal/worker`](internal/worker) | Run supervisor, [`acp`](internal/worker/acp) agent driver, [`sandbox`](internal/worker/sandbox) engines |
| [`internal/config`](internal/config) | Validated configuration for each binary |
| [`internal/testdb`](internal/testdb) | Per-test Postgres databases |
| [`web/`](web/) | Vite + React + TypeScript + Tailwind UI |
| [`deploy/compose`](deploy/compose) | Postgres + Server (+ worker) for one LAN host |
| [`desktop/`](desktop/) | Tauri shell, parked |
| [`docs/`](docs/) | Glossary, workflow, roadmap, ADRs |

## Configuration

| Variable | Binary | Default | Notes |
| --- | --- | --- | --- |
| `DATABASE_URL` | server | required | `postgres://…`; migrations run at startup |
| `BUILDBEE_ADDR` | server | `:8080` | listen address |
| `BUILDBEE_WEB_DIR` | server | embedded UI | serve the UI from a directory instead |
| `BUILDBEE_MAX_BODY_BYTES` | server | 32 MiB | request body limit |
| `GITHUB_TOKEN`, `GITHUB_REPO` | server | unset | draft PRs and Issue sync; without them those endpoints answer 503 |
| `GITHUB_WEBHOOK_SECRET` | server | unset | require `X-Hub-Signature-256` on webhooks |
| `BUILDBEE_URL` | worker, CLI | `http://127.0.0.1:8080` | Server to report to |
| `BUILDBEE_WORKER_ADDR` | worker | `127.0.0.1:8090` | Run endpoint |
| `BUILDBEE_FAKE_SANDBOX` | worker | `0` | `1` runs the fake engine instead of Docker |
| `BUILDBEE_WORKER_URL` | CLI | `http://127.0.0.1:8090` | worker used by `run start` |
| `BUILDBEE_TEST_DATABASE_URL` | tests | throwaway container | Postgres for `go test` |

Each binary validates its configuration at startup and refuses to start on bad input.

## Security model

BuildBee trusts the network it runs on. Anyone who can reach the Server can read and change every Project. Two protections remain:

- State-changing requests from another site's page are refused, and WebSockets only accept same-origin pages, so a website a LAN user visits cannot drive the Server through their browser.
- Only the Server port is published by compose. Postgres stays on the compose network, and the worker's Run endpoint binds to loopback.

A worker with the Docker socket mounted is root-equivalent on its host. Run workers only on machines dedicated to BuildBee.

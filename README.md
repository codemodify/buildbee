# BuildBee

BuildBee is a **Project** workspace where humans and **Bots** cooperate on engineering work.

This repository is the v0 monorepo scaffold: Server, web, CLI, and runtime stubs. Product nouns are locked in the [glossary](docs/glossary.md). See [team workflow](docs/workflow.md) and [ADR 0001](docs/adr/0001-stack.md) for the accepted stack.

License: [Apache-2.0](LICENSE).

## Monorepo map

| Path | What it is |
| --- | --- |
| [`server/`](server/) | Go **Server**: REST + WebSocket skeleton, `GET /healthz`, `/v1/` placeholder |
| [`web/`](web/) | Vite + React + TypeScript + Tailwind app |
| [`cli/`](cli/) | Go CLI (`buildbee`) |
| [`runtime/`](runtime/) | Sandbox + ACP **Run** supervisor stub |
| [`deploy/compose/`](deploy/compose/) | Docker Compose for Postgres 16 and the Server |
| [`docs/`](docs/) | Glossary, workflow, ADRs |

Go modules are separate (`github.com/codemodify/buildbee/server`, `.../cli`, `.../runtime`). A root [`go.work`](go.work) ties them together for local development.

```bash
# from the repo root
go work sync
```

## Run Server + web locally

Prerequisites: Go 1.22+, Node 20+, and (for Compose) Docker.

### 1. Server

```bash
cd server
go test ./...
go run ./cmd/server
```

The Server listens on `:8080` (override with `BUILDBEE_ADDR`).

- `GET http://localhost:8080/healthz`
- `GET http://localhost:8080/v1/`

Auth is a stub: GitHub OAuth for humans later; Bots get server-issued Identities.

### 2. Web

```bash
cd web
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173). Vite proxies `/healthz` and `/v1` to the Server.

```bash
npm run build   # production bundle
```

### 3. Optional: Postgres + Server via Compose

```bash
docker compose -f deploy/compose/docker-compose.yml up --build
```

MinIO (later Artifact storage) is behind the `extras` profile:

```bash
docker compose -f deploy/compose/docker-compose.yml --profile extras up --build
```

## CLI

```bash
cd cli
go run ./cmd/buildbee version
```

## Runtime

The runtime is a stub. Read [`runtime/README.md`](runtime/README.md) for the Sandbox + ACP Run lifecycle.

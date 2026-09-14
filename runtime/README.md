# Runtime

Go supervisor + Docker **Sandbox** (and optional **ACP** CLI) for **Runs**. Module: `github.com/codemodify/buildbee/runtime`

## ACP Run mode

`--acp` (or POST `/runs` with `"acp": true`) starts a short ACP session instead of a Docker command:

1. Detect an agent binary on `PATH`: `claude`, `codex`, `opencode`, `goose` (or `--agent` / `"agent"`).
2. Start session → send a prompt derived from Task title/body + Handoff notes → **stream** stdout/stderr (and FakeACP tokens / tool calls) as RunEvents.
3. Store Artifact **`acp.log`** (rolled-up transcript) and mark the Run succeeded/failed.

If no binary is present, or `--agent fake`, **FakeACP** streams chunks over ~1–2s and succeeds (CI/e2e).

Live events: `POST /v1/runs/{id}/events` · `GET /v1/runs/{id}/events?after=` · `GET /v1/runs/{id}/ws`. Kinds: `token`, `tool_call`, `tool_result`, `status`, `log`.

| Agent | Binary | Non-interactive invocation |
| --- | --- | --- |
| Claude | `claude` | `claude -p "<prompt>"` |
| Codex | `codex` | `codex exec "<prompt>"` |
| OpenCode | `opencode` | `opencode run "<prompt>"` |
| Goose | `goose` | `goose run -t "<prompt>"` |
| FakeACP | _(none)_ | in-process log |

Env / binaries:

| Name | Purpose |
| --- | --- |
| `PATH` | Must include the chosen ACP CLI for a real session |
| `BUILDBEE_URL` | Server the supervisor notifies (default `http://127.0.0.1:8080`) |
| `BUILDBEE_RUNTIME_ADDR` | Supervisor listen address (default `:8090`) |
| `BUILDBEE_FAKE_SANDBOX=1` | Force FakeEngine for the Docker path (not FakeACP) |

Without `--acp`, the supervisor uses Docker if available, else FakeEngine.

## Sandbox lifecycle

```
POST /v1/tasks/{taskID}/runs     → pending
        │
PATCH … status=running
        │
docker run --rm -w /work <image> sh -c '
  optional git clone $REPO
  $CMD
'
        │
POST /v1/tasks/{id}/artifacts    → sandbox.log (Artifact)
POST /v1/tasks/{id}/pr           → fake or real draft PR Artifact
PATCH … succeeded | failed
```

## Run locally

Server must be up. Fake engine (no Docker):

```bash
BUILDBEE_FAKE_SANDBOX=1 go run ./cmd/runtime
# POST http://127.0.0.1:8090/runs  {"task_id":"...","fake":true}
```

Real Docker:

```bash
go run ./cmd/runtime
# needs `docker` on PATH
```

CLI:

```bash
buildbee run start --task "$TASK_ID" --fake
buildbee run start --task "$TASK_ID" --acp --agent fake
buildbee run start --task "$TASK_ID" --acp --agent claude
buildbee run start --task "$TASK_ID" --repo-url https://github.com/org/repo.git --cmd "ls"
```

`fake-run` still exists:

```bash
go run ./cmd/fake-run --task "$TASK_ID"
```

Compose: `--profile runtime` (see `deploy/compose`). Default `BUILDBEE_FAKE_SANDBOX=1`.

## Packages

| Path | Role |
| --- | --- |
| `.` | Supervisor |
| [`acp/`](acp/) | ACP `Stream` / `Session`, Detect, FakeACP chunks |
| [`sandbox/`](sandbox/) | `Engine` interface, `DockerEngine`, `FakeEngine` |
| [`notify/`](notify/) | Server HTTP client |
| [`cmd/runtime/`](cmd/runtime/) | HTTP supervisor |
| [`cmd/fake-run/`](cmd/fake-run/) | Fake success helper |

```bash
go test ./...
```

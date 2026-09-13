# Runtime

Go supervisor + Docker **Sandbox** for ACP agent **Runs**.

Module: `github.com/codemodify/buildbee/runtime`

ACP and Docker Sandboxes are still stubs. The Server already stores **Run** records. This package posts status updates to that API.

## Sandbox + ACP Run lifecycle

```
Server records a Task Handoff to a Bot
        │
        ▼
POST /v1/tasks/{taskID}/runs     → Run pending
        │
Supervisor.StartRun (stub)
        │
Sandbox created (not implemented)
        │
ACP session (not implemented)
        │
PATCH /v1/runs/{runID}           → succeeded | failed | canceled
        │
Server appends Activity Type "run"
```

## Post a fake successful Run

With the Server running and a Task ID:

```bash
go run ./cmd/fake-run --task "$TASK_ID" --server http://127.0.0.1:8080
```

Or curl:

```bash
RUN_ID=$(curl -sS -X POST "http://127.0.0.1:8080/v1/tasks/$TASK_ID/runs" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
curl -sS -X PATCH "http://127.0.0.1:8080/v1/runs/$RUN_ID" \
  -H 'Content-Type: application/json' \
  -d '{"status":"succeeded","detail":"fake success (runtime stub)"}'
```

## Packages

| Path | Role |
| --- | --- |
| `.` | `Supervisor` and Run `Status` |
| [`sandbox/`](sandbox/) | Docker Sandbox spec (stub) |
| [`acp/`](acp/) | ACP session stub |
| [`notify/`](notify/) | HTTP client for Run status |
| [`cmd/fake-run/`](cmd/fake-run/) | CLI that posts fake success |

```bash
go test ./...
```

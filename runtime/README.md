# Runtime

Go supervisor + Docker **Sandbox** for ACP agent **Runs**.

Module: `github.com/codemodify/buildbee/runtime`

This directory is a stub. It documents the lifecycle and reserves package names. It does not start containers or speak ACP yet.

## Sandbox + ACP Run lifecycle

```
Server records a Task Handoff to a Bot
        │
        ▼
Supervisor.StartRun(runID)
        │
        ▼
Sandbox created (Docker container, isolated network + volume)
        │
        ▼
ACP session opened to the agent process inside the Sandbox
        │
        ├─ stream Activity (progress, logs) back to the Channel
        ├─ collect Artifacts (patches, reports)
        └─ exit status → Run succeeded | failed | canceled
        │
        ▼
Sandbox torn down; Server stores the Decision / Artifact links
```

| Stage | Noun | Notes |
| --- | --- | --- |
| Request | **Task**, **Handoff** | A Member hands work to a Bot. |
| Isolation | **Sandbox** | One Docker container per **Run**. |
| Protocol | ACP | Agent I/O inside the Sandbox. Not implemented here. |
| Output | **Artifact**, **Activity** | Streamed to the Server / Channel. |

## Packages

| Path | Role |
| --- | --- |
| `.` | `Supervisor` and Run `Status` |
| [`sandbox/`](sandbox/) | Docker Sandbox spec (stub) |
| [`acp/`](acp/) | ACP session stub |

```bash
go test ./...
```

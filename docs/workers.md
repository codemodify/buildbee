# Workers

A worker is a process that executes Runs. It pulls queued Runs from the Server, runs a coding agent on each, streams the agent's output back as RunEvents, and uploads the transcript as an Artifact. Workers open no port, so any machine on the LAN that can reach the Server can be one. Run as many as you like; each executes up to `BUILDBEE_WORKER_SLOTS` Runs at once.

## How a Run reaches a worker

1. Someone queues a Run: `POST /v1/tasks/{id}/runs`, `buildbee run start`, or a Handoff to the Builder with autorun. The Run is `pending`, with the agent to use and the prompt (the Bot's instructions, the Task, and the Handoff notes addressed to that Bot).
2. A worker long-polls `POST /v1/worker/claim` with the agents it offers. The Server hands the oldest matching Run to exactly one worker, marks it `running`, and leases it for 60 seconds. A Run queued while workers wait wakes them at once.
3. The worker heartbeats every 15 seconds (`POST /v1/runs/{id}/heartbeat`). The answer carries the Run's status, so a Run canceled by a person stops within one heartbeat.
4. When the agent finishes, the worker uploads `<agent>.log` and marks the Run `succeeded` or `failed`.
5. If a worker stops heartbeating (crash, power loss, network), the Server fails its Runs once the lease runs out. Agent work is not safe to repeat blindly, so nothing is retried automatically; queue the Run again.

Only the worker that claimed a Run can write to it. People can still cancel any Run.

## Which Runs a worker takes

A Run names an agent (`claude`, `codex`, `opencode`, `goose`, `grok`, or `fake`), taken from the request or from the Bot's `agent` setting (`PATCH /v1/members/{id}`). A Run with no agent goes to any worker offering a real one. The `fake` agent streams canned output and only takes Runs that ask for it: it exists for demos and tests.

## Running a worker

On a machine where your agent CLIs are installed and logged in:

```bash
BUILDBEE_URL=http://buildbee.lan:8080 \
BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1 \
buildbee-worker
```

| Variable | Default | Notes |
| --- | --- | --- |
| `BUILDBEE_URL` | `http://127.0.0.1:8080` | the Server |
| `BUILDBEE_WORKER_NAME` | hostname | must be unique on the LAN; Runs are owned by name |
| `BUILDBEE_WORKER_AGENTS` | see below | comma-separated agents to offer |
| `BUILDBEE_WORKER_SLOTS` | `4` | Runs executed at once (1–256) |
| `BUILDBEE_WORKER_ALLOW_HOST_AGENTS` | `0` | `1` lets real agent CLIs run on this host |

Without `BUILDBEE_WORKER_AGENTS`, a worker allowed to run host agents offers every supported agent CLI on its `PATH`; otherwise it offers only `fake`. A worker refuses to start if asked to offer a real agent without `BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1`.

The compose file's `worker` profile starts a worker that offers only `fake`, for demos and the smoke test.

## Agent logins

BuildBee stores no agent credentials or API keys. An agent uses whatever login its CLI already has on the worker machine (for example `~/.claude`, `~/.codex`), exactly as when you run it yourself. Parallel Runs on one login share that subscription's usage limits.

## Current limits

Host agents run as the worker's user, in an empty temporary directory per Run, with that user's files and logins. Until Runs execute in per-Run containers with a checkout of the Project's repo (roadmap Phase 3), run workers with host agents only on machines and accounts you are comfortable handing to an agent.

# Workers

A worker is a process that executes Runs. It pulls queued Runs from the Server, runs a coding agent on each, streams the agent's output back as RunEvents, and uploads the transcript as an Artifact. Workers open no port, so any machine on the LAN that can reach the Server can be one. Run as many as you like; each executes up to `BUILDBEE_WORKER_SLOTS` Runs at once.

Agents are programs, not network services: each Run needs a machine with the agent installed, logged in (BuildBee uses the CLI logins, not API keys), a checkout of the repo, and a sandbox. That is what a worker provides. More workers mean more Runs at once and more logins to spread them over.

## The Server's own worker

The Server runs a worker itself, so on one machine there is nothing else to start. At startup it looks for, in order:

1. Docker and the agent image (`make agents-image`): each Run gets a container, with the agents the Server's user is logged in to.
2. Otherwise agents installed on the machine: they run directly, as the Server's user, with its files and logins.

If neither is there, the Server runs no agents and `# status` says why. `BUILDBEE_LOCAL_WORKER` is `auto` (the default), `on` (refuse to start without agents) or `off`. The worker's other settings are the `BUILDBEE_WORKER_*` variables below; its name defaults to the hostname with `-server`, and `BUILDBEE_WORKER_ISOLATION` pins the isolation instead of falling back. Other machines join with `buildbee-worker` as below.

`# status` shows each agent's load (running and queued Runs, and how many machines offer it) and warns when Runs or Bots wait for an agent no machine offers.

## How a Run reaches a worker

1. Someone queues a Run: `POST /v1/tasks/{id}/runs`, `buildbee run start`, or a Handoff to the Builder with autorun. The Run is `pending`, with the agent to use and the prompt (the Bot's instructions, the Task, and the Handoff notes addressed to that Bot).
2. A worker long-polls `POST /v1/worker/claim` with the agents it offers. The Server hands the oldest matching Run to exactly one worker, marks it `running`, and leases it for 60 seconds. A Run queued while workers wait wakes them at once.
3. The worker heartbeats every 15 seconds (`POST /v1/runs/{id}/heartbeat`). The answer carries the Run's status, so a Run canceled by a person stops within one heartbeat.
4. When the agent finishes, the worker uploads `<agent>.log` and marks the Run `succeeded` or `failed`.
5. If a worker stops heartbeating (crash, power loss, network), the Server fails its Runs once the lease runs out. Agent work is not safe to repeat blindly, so nothing is retried automatically; queue the Run again.

Only the worker that claimed a Run can write to it. People can still cancel any Run, and message the agent working on it (`POST /v1/runs/{id}/steer`, `buildbee run steer`): the worker follows the Run's event stream and gives the message to the agent as its next turn, or at once with `interrupt`.

Projects share the workers. A Project with `max_runs` set never has more Runs going at once; among the rest, the Project with the fewest Runs going is served first, then the oldest Run.

## Which Runs a worker takes

A Run names an agent (`claude`, `codex`, `opencode`, `goose`, `grok`, or `fake`), taken from the request or from the Bot's `agent` setting (`PATCH /v1/members/{id}`). A Run with no agent goes to any worker offering a real one. The `fake` agent streams canned output and only takes Runs that ask for it: it exists for demos and tests.

## Running a worker

On a machine with Docker, where your agent CLIs are logged in:

```bash
make agents-image   # docker build -f Dockerfile.agents -t buildbee-agents .
BUILDBEE_URL=http://buildbee.lan:8080 buildbee-worker
```

The worker finds the agents in the image, and each Run gets a container of its own (see [Isolation](#isolation)). To run agents directly on the machine instead, as Buzz does, set `BUILDBEE_WORKER_ISOLATION=host`; the agents' ACP commands must then be on the worker's `PATH`.

| Variable | Default | Notes |
| --- | --- | --- |
| `BUILDBEE_URL` | `http://127.0.0.1:8080` | the Server |
| `BUILDBEE_WORKER_NAME` | hostname | must be unique on the LAN; Runs are owned by name |
| `BUILDBEE_WORKER_AGENTS` | see below | comma-separated agents to offer |
| `BUILDBEE_WORKER_SLOTS` | `4` | Runs executed at once (1–256) |
| `BUILDBEE_WORKER_ISOLATION` | `container` | `container`: one container per Run; `host`: agents run on this machine as the worker's user |
| `BUILDBEE_WORKER_IMAGE` | `buildbee-agents` | image with the agents' ACP commands |
| `BUILDBEE_WORKER_MEMORY`, `BUILDBEE_WORKER_CPUS` | `8g`, `4` | limits per Run container |
| `BUILDBEE_WORKER_NETWORK` | Docker's default | network for Run containers |
| `BUILDBEE_WORKER_RUN_TIMEOUT` | `2h` | a Run that takes longer is stopped and failed |
| `BUILDBEE_WORKER_DIR` | `~/.cache/buildbee-worker` | repo mirrors and Run checkouts |
| `BUILDBEE_WORKER_OPEN_PRS` | `1` | `0` pushes branches without opening pull requests |
| `BUILDBEE_AGENT_<NAME>` | see below | command that starts an agent, e.g. `BUILDBEE_AGENT_CLAUDE="npx -y @agentclientprotocol/claude-agent-acp"` |

Without `BUILDBEE_WORKER_AGENTS`, a worker offers every agent whose ACP command it finds: in the image, or on its `PATH` in host isolation. In a container only agents with a login in the worker user's home count (see [Agent logins](#agent-logins)). It refuses to start when an agent it is asked to offer is missing. `BUILDBEE_WORKER_AGENTS=fake` needs neither Docker nor agents.

## Agents

Workers talk to agents over the [Agent Client Protocol](https://agentclientprotocol.com) (ACP), the same way Zed and Block's Buzz do: one agent process per Run, JSON-RPC over its stdin and stdout.

| Agent | Command | Notes |
| --- | --- | --- |
| `claude` | `claude-agent-acp` | adapter for Claude Code: `npm install -g @agentclientprotocol/claude-agent-acp` |
| `codex` | `codex-acp` | adapter for Codex: `npm install -g @zed-industries/codex-acp` |
| `grok` | `grok agent --always-approve --no-leader stdio` | built into the Grok CLI |
| `opencode` | `opencode acp` | built in |
| `goose` | `goose acp` | built in |

For each Run the worker starts the agent in the Run's directory, opens a session, sends the prompt, and streams what the agent does as RunEvents: its reply (`token`), reasoning (`thought`), `plan`, `tool_call` and `tool_result`, context `usage`, and its stderr as `log`. Reply chunks are merged, so a Run posts a few events per second rather than one per token.

Agents run unattended. The worker switches the session to a mode that skips permission prompts when the agent offers one (for example `bypassPermissions`), and approves, once, any permission the agent still asks for; each approval is logged on the Run. Canceling a Run sends `session/cancel`, waits 10 seconds, then kills the agent and every process it started.

If an agent answers that it needs a login, the Run fails with a message saying so: log in to that CLI on the worker machine, as the worker's user.

The compose file's `worker` profile starts a worker that offers only `fake`, for demos and the smoke test.

## Isolation

With `BUILDBEE_WORKER_ISOLATION=container` (the default), each Run's agent runs in a fresh container started from `BUILDBEE_WORKER_IMAGE`, and talks ACP over the container's stdin and stdout. The container:

- runs as the worker's user, with every capability dropped, `no-new-privileges`, and memory, CPU and process limits;
- sees the Run's own repo (at its host path), the mirror's object store read-only (the repo borrows its objects), and a scratch home directory;
- gets copies of its agent's login files in that home (for example `~/.claude/.credentials.json` and `~/.claude.json` for Claude Code, `~/.codex/auth.json` and `config.toml` for Codex), never the host's agent directories;
- has no Docker socket, and is removed when the Run ends, is canceled, or the worker restarts.

When the Run ends, a token the agent refreshed is written back to the host, if it is well-formed and nobody changed the host file meanwhile. Nothing else the container writes reaches the host's agent configuration, so an agent cannot leave hooks or settings behind.

The worker never runs git in the agent's repo once the agent has started. It records the agent's files into a worktree of its own (`git --work-tree`) and commits, diffs and pushes from there, so hooks, config or a redirected `.git` the agent wrote are never used. The agent's own commits are not kept; its files are, as one commit per build. Host git also runs with hooks and file-system monitors disabled.

`Dockerfile.agents` builds an image with `claude-agent-acp`, `codex-acp`, OpenCode and Grok on Node 24 with git, Python and a C toolchain. A Project that needs more (Go, Rust, database clients) gets its own image: build one `FROM buildbee-agents`, push it where workers can pull, and set it as the Project's agent image (Settings → Repository, or `agent_image` on `PATCH /v1/projects/{id}`). A worker pulls a Project's image the first time it needs it and checks that the Run's agent is in it; a Run whose image cannot be pulled or lacks its agent fails and says so. `BUILDBEE_WORKER_IMAGE` stays the default for Projects without one.

With `BUILDBEE_WORKER_ISOLATION=host`, agents run directly on the machine as the worker's user. On Linux with [bubblewrap](https://github.com/containers/bubblewrap) installed they are sandboxed (`BUILDBEE_WORKER_SANDBOX=auto`, the default): the system is read-only, the user's home is hidden (agent installs under it, such as `~/.local` or `~/.nvm`, come back read-only), the agent writes only its Run's checkout and a scratch home with copies of its login files (refreshed tokens are written back as for containers), and it gets its own process namespace; the network stays open for the model. `require` refuses to start without the sandbox; `off`, or a machine without bubblewrap (macOS, Windows), runs agents with that user's files, tools and logins, like Buzz, and says so in the log and `# status`. Use that only on machines and accounts you are comfortable handing to an agent.

## Permissions

Agents ask before some actions (editing files, running commands). By default a worker approves each request, preferring "once" over "always", and switches agents to a mode that skips asking: a Run's agent works in its own checkout and container. A Project set to `agent_permissions: ask` (Settings → Autopilot) keeps agents in their asking mode instead: each request becomes a Decision on the Task ("Builder asks to: Edit src/app.ts", with the agent's own options), shown in the Task's thread and `# decisions`, and the agent waits until someone answers. A Run that ends, or is canceled, closes the questions it left open.

## Repos, branches and pull requests

When a Project has a `repo_url` (`PATCH /v1/projects/{id}`), every Run works in a fresh checkout of it:

1. The worker keeps one bare mirror per repo in `BUILDBEE_WORKER_DIR`, fetches it, and adds a git worktree for the Run on a new branch `buildbee/<task-title>-<run-id>`, starting from the tip of the Project's `default_branch` (or the repo's default).
2. The agent works in that checkout. Parallel Runs on one repo each have their own.
3. When the agent finishes, the worker commits anything it left uncommitted, uploads `changes.diff` as an Artifact, and pushes the branch.
4. For GitHub repos, it opens a pull request with the GitHub CLI (`gh pr create`) and records it as a `pr` Artifact.

The worker pushes with its own git credentials (SSH keys, credential helper) and opens pull requests with its own `gh` login, the same way agents use their own logins. If the push fails, the Run fails and the branch stays in the worker's mirror so nothing is lost. A Run that changes nothing succeeds without a branch.

Pull requests are opened and merged by workers only; the Server holds no GitHub write access. Git runs without prompts and only over `file`, `git`, `http`, `https` and `ssh`. The Server refuses repo URLs git could read as an option or a remote helper.

## Agent logins

BuildBee stores no agent credentials or API keys. An agent uses whatever login its CLI already has on the worker machine (for example `~/.claude`, `~/.codex`), exactly as when you run it yourself. Parallel Runs on one login share that subscription's usage limits.

## Current limits

- Containers get network access (agents need their model APIs); restrict it with `BUILDBEE_WORKER_NETWORK` and your firewall.
- One image serves all Projects on a worker.

# Agents

A **Bot** is a member of a Project: a name, a role and standing instructions. A **buildbee-agent** is the process that brings that Bot to life on a machine — it runs one AI's CLI (`claude`, `codex`, `grok`, `opencode`, `goose`), takes that Bot's Runs from the Server, and passes the conversation between them. Bot and agent are two sides of one thing: the Bot is what you @mention, the agent is what runs.

There is no pool of machines and no matching to think about. Adding a Bot means running its agent somewhere:

```bash
buildbee-agent --server http://buildbee.lan:8080 --bot <bot id> --ai claude
```

**Add bot** in `# status` creates the Bot and shows that line ready to paste, with the Bot's id filled in. Run it where that AI is logged in — a laptop, a build box, the Server's own machine. The Bot is **online** while the agent runs and **offline** when it stops; `# status` says which, what AI it runs, where, and how many Runs it has going. Work queued for an offline Bot waits (and `# status` says so with the command to fix it).

One agent, one Bot, one AI. For a Bot that should work on several things at once, give its agent more slots (`--slots`, default 4). The agent opens no port; it connects out to the Server, so it works from anywhere that can reach the LAN address.

## How a Run reaches its Bot

1. Someone queues a Run: an @mention of the Bot, a Handoff to it with autorun, `POST /v1/tasks/{id}/runs`, or `buildbee run start`. The Run is `pending`, with the prompt (the Bot's instructions, the Task, the notes handed to it) and the Task's attachments.
2. The Bot's agent long-polls `POST /v1/agent/claim`, saying which AI it runs, which machine it is on and how many Runs it takes. The Server hands it the oldest Run for that Bot, marks it `running` and leases it for 60 seconds. A Run queued while the agent waits wakes it at once. A Run nobody is assigned (a merge) goes to whichever agent asks first.
3. The agent heartbeats every 15 seconds (`POST /v1/runs/{id}/heartbeat`). The answer carries the Run's status, so a Run canceled by a person stops within one heartbeat.
4. When the AI finishes, the agent uploads `<ai>.log` and marks the Run `succeeded` or `failed`.
5. If an agent stops heartbeating (crash, power loss, network), the Server fails its Runs once the lease runs out. AI work is not safe to repeat blindly, so nothing is retried automatically; queue the Run again.

Only the agent that claimed a Run can write to it. People can still cancel any Run, and message the AI working on it (`POST /v1/runs/{id}/steer`, `buildbee run steer`): the agent follows the Run's event stream and gives the message to the AI as its next turn, or at once with `interrupt`.

Projects share their Bots' attention: a Project with `max_runs` set never has more Runs going at once; among the rest, the Project with the fewest Runs going is served first, then the oldest Run.

## Starting an agent

On a machine with Docker, where the AI's CLI is logged in:

```bash
make agents-image   # docker build -f Dockerfile.agents -t buildbee-agents .
buildbee-agent --server http://buildbee.lan:8080 --bot 7f3a... --ai claude
```

Each Run then gets a container of its own (see [Isolation](#isolation)). To run the AI directly on the machine instead, as Buzz does, add `--isolation host`; its ACP command must then be on the agent's `PATH`.

Every flag is also an environment variable, for compose and systemd:

| Variable | Flag | Default | Notes |
| --- | --- | --- | --- |
| `BUILDBEE_URL` | `--server` | `http://127.0.0.1:8080` | the Server |
| `BUILDBEE_BOT` | `--bot` | required | the Bot this agent runs |
| `BUILDBEE_AI` | `--ai` | required | `claude`, `codex`, `grok`, `opencode`, `goose` or `fake` |
| `BUILDBEE_AGENT_NAME` | `--name` | `<bot>@<host>` | how this agent names itself on Runs |
| `BUILDBEE_AGENT_HOST` | | the hostname | the machine, for people to see |
| `BUILDBEE_SLOTS` | `--slots` | `4` | Runs this Bot takes at once (1–256) |
| `BUILDBEE_ISOLATION` | `--isolation` | `container` | `container`: one container per Run; `host`: on this machine |
| `BUILDBEE_SANDBOX` | `--sandbox` | `auto` | bubblewrap for host isolation: `auto`, `require` or `off` |
| `BUILDBEE_IMAGE` | `--image` | `buildbee-agents` | image with the AIs' ACP commands |
| `BUILDBEE_MEMORY`, `BUILDBEE_CPUS` | | `8g`, `4` | limits per Run container |
| `BUILDBEE_NETWORK` | | Docker's default | network for Run containers |
| `BUILDBEE_RUN_TIMEOUT` | | `2h` | a Run that takes longer is stopped and failed |
| `BUILDBEE_DIR` | `--dir` | `~/.cache/buildbee-agent` | repo mirrors and Run checkouts |
| `BUILDBEE_OPEN_PRS` | | `1` | `0` pushes branches without opening pull requests |
| `BUILDBEE_AI_<NAME>` | | see below | command that starts an AI, e.g. `BUILDBEE_AI_CLAUDE="npx -y @agentclientprotocol/claude-agent-acp"` |

An agent refuses to start when its AI has no ACP command here, or (in a container) no login for this user: it says which, rather than taking work it cannot do. `--ai fake` needs neither Docker nor a login.

## The AIs

An agent talks to its AI over the [Agent Client Protocol](https://agentclientprotocol.com) (ACP), the same way Zed and Block's Buzz do: one AI process per Run, JSON-RPC over its stdin and stdout.

| AI | Command | Notes |
| --- | --- | --- |
| `claude` | `claude-agent-acp` | adapter for Claude Code: `npm install -g @agentclientprotocol/claude-agent-acp` |
| `codex` | `codex-acp` | adapter for Codex: `npm install -g @zed-industries/codex-acp` |
| `grok` | `grok agent --always-approve --no-leader stdio` | built into the Grok CLI |
| `opencode` | `opencode acp` | built in |
| `goose` | `goose acp` | built in |

For each Run the agent starts its AI in the Run's directory, opens a session, sends the prompt, and streams what the agent does as RunEvents: its reply (`token`), reasoning (`thought`), `plan`, `tool_call` and `tool_result`, context `usage`, and its stderr as `log`. Reply chunks are merged, so a Run posts a few events per second rather than one per token.

AIs run unattended unless the Project says otherwise. The agent switches the session to a mode that skips permission prompts when the AI offers one (for example `bypassPermissions`), and approves, once, any permission the AI still asks for; each approval is logged on the Run. Canceling a Run sends `session/cancel`, waits 10 seconds, then kills the AI and every process it started.

If an AI answers that it needs a login, the Run fails with a message saying so: log in to that CLI on the agent's machine, as the agent's user.

The compose file's `agent` profile starts an agent running the `fake` AI, for demos and the smoke test.

## Isolation

With `BUILDBEE_ISOLATION=container` (the default), each Run's AI runs in a fresh container started from `BUILDBEE_IMAGE`, and talks ACP over the container's stdin and stdout. The container:

- runs as the agent's user, with every capability dropped, `no-new-privileges`, and memory, CPU and process limits;
- sees the Run's own repo (at its host path), the mirror's object store read-only (the repo borrows its objects), and a scratch home directory;
- gets copies of its AI's login files in that home (for example `~/.claude/.credentials.json` and `~/.claude.json` for Claude Code, `~/.codex/auth.json` and `config.toml` for Codex), never the host's AI directories;
- has no Docker socket, and is removed when the Run ends, is canceled, or the agent restarts.

When the Run ends, a token the AI refreshed is written back to the host, if it is well-formed and nobody changed the host file meanwhile. Nothing else the container writes reaches the host's agent configuration, so an AI cannot leave hooks or settings behind.

The agent never runs git in the AI's repo once the agent has started. It records the agent's files into a worktree of its own (`git --work-tree`) and commits, diffs and pushes from there, so hooks, config or a redirected `.git` the agent wrote are never used. The agent's own commits are not kept; its files are, as one commit per build. Host git also runs with hooks and file-system monitors disabled.

`Dockerfile.agents` builds an image with `claude-agent-acp`, `codex-acp`, OpenCode and Grok on Node 24 with git, Python and a C toolchain. A Project that needs more (Go, Rust, database clients) gets its own image: build one `FROM buildbee-agents`, push it where agents can pull, and set it as the Project's agent image (Settings → Repository, or `agent_image` on `PATCH /v1/projects/{id}`). An agent pulls a Project's image the first time it needs it and checks its AI is in it; a Run whose image cannot be pulled, or whose image lacks that AI, fails and says so. `BUILDBEE_IMAGE` stays the default for Projects without one.

With `BUILDBEE_ISOLATION=host`, agents run directly on the machine as the agent's user. On Linux with [bubblewrap](https://github.com/containers/bubblewrap) installed they are sandboxed (`BUILDBEE_SANDBOX=auto`, the default): the system is read-only, the user's home is hidden (agent installs under it, such as `~/.local` or `~/.nvm`, come back read-only), the agent writes only its Run's checkout and a scratch home with copies of its login files (refreshed tokens are written back as for containers), and it gets its own process namespace; the network stays open for the model. `require` refuses to start without the sandbox; `off`, or a machine without bubblewrap (macOS, Windows), runs agents with that user's files, tools and logins, like Buzz, and says so in the log and `# status`. Use that only on machines and accounts you are comfortable handing to an agent.

## What the agent is given

A Run's prompt carries the Project's guidance, the Bot's instructions, what the Run is for, the Task and the notes handed to the Bot. What people attached to the Task comes too: the claim lists the files, the agent downloads them beside its AI (in its home in a container or sandbox, in a scratch directory otherwise) and names them in the prompt with their paths. Images also go into the prompt as pictures when the AI says it reads them (`promptCapabilities.image`), so a pasted screenshot is something the AI can look at, not just a path. At most 20 files and 64 MB per Run; anything larger is skipped and said so in the Run's log.

## Permissions

AIs ask before some actions (editing files, running commands). By default the agent approves each request, preferring "once" over "always", and switches the AI to a mode that skips asking: it works in its own checkout and container anyway. A Project set to `agent_permissions: ask` (Settings → Autopilot) keeps the AI in its asking mode instead: each request becomes a Decision on the Task ("Builder asks to: Edit src/app.ts", with the AI's own options), shown in the Task's thread and `# decisions`, and the AI waits until someone answers. A Run that ends, or is canceled, closes the questions it left open.

## Repos, branches and pull requests

When a Project has a `repo_url` (`PATCH /v1/projects/{id}`), every Run works in a fresh checkout of it:

1. The agent keeps one bare mirror per repo in `BUILDBEE_DIR`, fetches it, and adds a git worktree for the Run on a new branch `buildbee/<task-title>-<run-id>`, starting from the tip of the Project's `default_branch` (or the repo's default).
2. The agent works in that checkout. Parallel Runs on one repo each have their own.
3. When the agent finishes, the agent commits anything it left uncommitted, uploads `changes.diff` as an Artifact, and pushes the branch.
4. For GitHub repos, it opens a pull request with the GitHub CLI (`gh pr create`) and records it as a `pr` Artifact.

The agent pushes with its own git credentials (SSH keys, credential helper) and opens pull requests with its own `gh` login, the same way agents use their own logins. If the push fails, the Run fails and the branch stays in the agent's mirror so nothing is lost. A Run that changes nothing succeeds without a branch.

Pull requests are opened and merged by agents only; the Server holds no GitHub write access. Git runs without prompts and only over `file`, `git`, `http`, `https` and `ssh`. The Server refuses repo URLs git could read as an option or a remote helper.

## AI logins

BuildBee stores no credentials or API keys. An AI uses whatever login its CLI already has on the agent's machine (for example `~/.claude`, `~/.codex`), exactly as when you run it yourself. Parallel Runs on one login share that subscription's usage limits.

## Current limits

- Containers get network access (agents need their model APIs); restrict it with `BUILDBEE_NETWORK` and your firewall.
- One image serves every Project an agent works for, unless the Project names its own.

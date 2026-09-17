# Glossary v1

Locked product language for BuildBee. Use these nouns in docs, APIs, and code comments. Do not invent synonyms.

## Workspace

| Noun | Meaning |
| --- | --- |
| **Project** | The workspace where humans and Bots cooperate on engineering work. |
| **Channel** | A conversation and coordination surface inside a Project. |
| **Bot** | An automated collaborator that holds an Identity and can take on Tasks: a name, a Role and standing instructions in one Project. Its **Agent** runs it on some machine; the Bot's work goes only there. |
| **Decisions** | The Project record of choices the team has committed to. |

## People and access

| Noun | Meaning |
| --- | --- |
| **Person** | Someone using BuildBee, identified by the name they chose. A Person is a Member of each Project they work in. |
| **Member** | A Person or a Bot in one Project. |
| **Role** | What a Member does in a Project. Humans start as **owner**. A Project starts with no Bots; the autopilot's are offered when adding one: **Scout** (plans), **Builder** (implements and pushes), **Sentry** (reviews), **Pulse** (opens the Tasks Routines schedule). |
| **Identity** | Who is acting: a Person (by the name they chose — there is no login on a trusted LAN), a Bot through its Agent, or a named system process. AI credentials stay on the machines their agents run on, never in BuildBee. |

## Work

| Noun | Meaning |
| --- | --- |
| **Task** | A unit of work owned by a Member. |
| **Handoff** | Transfer of a Task (or context for a Task) between Members. |
| **Artifact** | A file or output produced while doing work (code, logs, reports). |
| **Run** | One execution on a Task, queued on the Server and claimed by one Bot's Agent. Its **kind** is `plan`, `build`, `review` (with a verdict) or `merge`. Incremental output is stored as RunEvents on that Run; the transcript is an Artifact. |
| **Autopilot** | A Project setting (`auto_run`) under which Bots move each Task from plan to merged code by themselves; see [autopilot.md](autopilot.md). |
| **Steering** | A person's message to the AI working on a Run, delivered as its next turn or at once. |
| **Agent** | `buildbee-agent`: the process that brings one Bot to life. It runs that Bot's **AI**, takes the Bot's Runs from the Server, heartbeats while they execute, and reports RunEvents, Artifacts and the outcome. A Bot is online while its agent runs. Bot and agent are two sides of one thing: the Bot is what you @mention, the agent is what runs. |
| **AI** | The program that does the thinking inside a Run: `claude`, `codex`, `grok`, `opencode`, `goose` (and `fake` for demos), started over ACP by the agent on a machine where that CLI is logged in. |
| **Decision** | A single recorded choice; the Decisions surface collects them. A Decision with an **action** (`merge`) does something when answered. |

## Platform

| Noun | Meaning |
| --- | --- |
| **Routine** | A scheduled prompt: each firing opens a Task with it and hands it to a Bot. |
| **Repo** | Source control attached to a Project (GitHub). |
| **Issues** | Tracked work items from the attached Repo. |
| **Pipelines** | CI/CD attached to the Repo. |
| **Sandbox** | What a Run executes in: a container per Run (the Run's checkout, a scratch home and its AI's login, nothing else), or bubblewrap when the AI runs on the machine itself. |
| **Activity** | An event in a Project. Later Activity may be signed (Nostr-inspired). This scaffold does not implement NIP-01. |
| **Type** | A named kind of Project object (Task, Artifact, Decision, and so on). |
| **Server** | The BuildBee backend: REST, WebSocket, Postgres persistence and the web UI. |
| **Notification** | An inbox item for a Member (Decision pending, Handoff to you, @mention Task, Pipeline failure). |

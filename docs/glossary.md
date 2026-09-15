# Glossary v1

Locked product language for BuildBee. Use these nouns in docs, APIs, and code comments. Do not invent synonyms.

## Workspace

| Noun | Meaning |
| --- | --- |
| **Project** | The workspace where humans and Bots cooperate on engineering work. |
| **Channel** | A conversation and coordination surface inside a Project. |
| **Bot** | An automated collaborator that holds an Identity and can take on Tasks. |
| **Decisions** | The Project record of choices the team has committed to. |

## People and access

| Noun | Meaning |
| --- | --- |
| **Person** | Someone using BuildBee, identified by the name they chose. A Person is a Member of each Project they work in. |
| **Member** | A Person or a Bot in one Project. |
| **Role** | What a Member does in a Project. Humans start as **owner**. Seeded Bots: **Scout** (plans), **Builder** (implements and pushes), **Sentry** (reviews), **Pulse** (opens the Tasks Routines schedule). |
| **Identity** | Who is acting: a Person (by the name they chose — there is no login on a trusted LAN), a Bot, or a named system process such as a worker. Agent credentials stay on worker machines, never in BuildBee. |

## Work

| Noun | Meaning |
| --- | --- |
| **Task** | A unit of work owned by a Member. |
| **Handoff** | Transfer of a Task (or context for a Task) between Members. |
| **Artifact** | A file or output produced while doing work (code, logs, reports). |
| **Run** | One execution on a Task, queued on the Server and claimed by one Worker. Its **kind** is `plan`, `build`, `review` (with a verdict) or `merge`. Incremental output is stored as RunEvents on that Run; the transcript is an Artifact. |
| **Autopilot** | A Project setting (`auto_run`) under which Bots move each Task from plan to merged code by themselves; see [autopilot.md](autopilot.md). |
| **Steering** | A person's message to the agent working on a Run, delivered as the agent's next turn or at once. |
| **Worker** | A process on any LAN machine that claims queued Runs for the agents it offers, heartbeats while they execute, and reports RunEvents, Artifacts and the outcome to the Server. |
| **Decision** | A single recorded choice; the Decisions surface collects them. A Decision with an **action** (`merge`) does something when answered. |

## Platform

| Noun | Meaning |
| --- | --- |
| **Routine** | A scheduled prompt: each firing opens a Task with it and hands it to a Bot. |
| **Repo** | Source control attached to a Project (GitHub). |
| **Issues** | Tracked work items from the attached Repo. |
| **Pipelines** | CI/CD attached to the Repo. |
| **Sandbox** | The per-Run container a Run executes in: the Run's checkout, a scratch home and its agent's login, nothing else from the worker host. |
| **Activity** | An event in a Project. Later Activity may be signed (Nostr-inspired). This scaffold does not implement NIP-01. |
| **Type** | A named kind of Project object (Task, Artifact, Decision, and so on). |
| **Server** | The BuildBee backend: REST, WebSocket, Postgres persistence and the web UI. |
| **Notification** | An inbox item for a Member (Decision pending, Handoff to you, @mention Task, Pipeline failure). |

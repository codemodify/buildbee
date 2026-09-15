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
| **Role** | What a Member does in a Project. Humans start as **owner**. Seeded Bots: **Scout** (triage), **Builder** (implement), **Sentry** (review/CI), **Pulse** (Routines/digests). |
| **Identity** | Who is acting: a Person (by the name they chose — there is no login on a trusted LAN), a Bot, or a named system process such as a worker. Agent credentials stay on worker machines, never in BuildBee. |

## Work

| Noun | Meaning |
| --- | --- |
| **Task** | A unit of work owned by a Member. |
| **Handoff** | Transfer of a Task (or context for a Task) between Members. |
| **Artifact** | A file or output produced while doing work (code, logs, reports). |
| **Run** | One execution of a Bot in a Sandbox. Incremental ACP output is stored as RunEvents on that Run; the rolled-up transcript is an Artifact. |
| **Worker** | A process on a machine with Docker that executes Runs and reports status, RunEvents and Artifacts to the Server. |
| **Decision** | A single recorded choice; the Decisions surface collects them. |

## Platform

| Noun | Meaning |
| --- | --- |
| **Routine** | A repeatable Project workflow (scheduled or triggered). |
| **Repo** | Source control attached to a Project (GitHub). |
| **Issues** | Tracked work items from the attached Repo. |
| **Pipelines** | CI/CD attached to the Repo. |
| **Sandbox** | Isolated Docker environment where a Bot Run executes. |
| **Activity** | An event in a Project. Later Activity may be signed (Nostr-inspired). This scaffold does not implement NIP-01. |
| **Type** | A named kind of Project object (Task, Artifact, Decision, and so on). |
| **Server** | The BuildBee backend: REST, WebSocket, Postgres persistence and the web UI. |
| **Notification** | An inbox item for a Member (Decision pending, Handoff to you, @mention Task, Pipeline failure). |

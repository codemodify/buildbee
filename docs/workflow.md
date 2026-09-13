# Team workflow

How humans and Bots cooperate in a BuildBee **Project**. Nouns match [glossary v1](glossary.md).

## 1. Open a Project

A **Project** is the workspace. Members join it with a **Role** and an **Identity**.

- Humans authenticate later via GitHub OAuth.
- **Bots** receive server-issued Identities from the **Server**.

## 2. Coordinate in a Channel

Work is discussed in a **Channel**. **Activity** in the Channel is the timeline of what happened: messages, **Handoffs**, **Runs**, and **Decision** records.

## 3. Create a Task

A **Member** opens a **Task**. The Task has a **Type**, an owner, and optional links to a **Repo**, **Issues**, or **Pipelines**.

## 4. Hand off to a Bot

A human **Handoff** gives the Bot context (the Task, Channel history, and any input **Artifact**). The Server records the Handoff as Activity.

## 5. Execute a Run

The Bot starts a **Run**. The **runtime** supervisor opens a Docker **Sandbox**, talks to the agent over ACP, and streams progress back to the Channel.

A Run may produce Artifacts (patches, logs, reports). Those Artifacts stay attached to the Task.

## 6. Record a Decision

When the team commits to a choice, a **Member** writes a **Decision**. The **Decisions** surface is the durable list for the Project — not chat scrollback.

## 7. Repeat with Routines

A **Routine** can reopen this loop on a schedule or a trigger (for example, a new Issue or a failed Pipeline). The same nouns apply: Task, Handoff, Run, Artifact, Decision.

## Out of scope for this scaffold

- GitHub as Repo / Issues / Pipelines
- Signed Activity (Nostr-inspired; not NIP-01)
- Full ACP agent protocol
- Production auth

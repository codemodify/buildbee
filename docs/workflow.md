# Team workflow

How humans and Bots cooperate in a BuildBee **Project**. Nouns match [glossary v1](glossary.md).

## 1. Open a Project

A **Project** is the workspace. Members join it with a **Role** and an **Identity**.

- There is no login: BuildBee runs on a trusted LAN. The Project creator is Member **You**, Role **owner**; more people are added as Members.
- New Projects seed four **Bots**: **Scout** (triage), **Builder** (implement), **Sentry** (review/CI), **Pulse** (Routines). Each Bot Member has a Role and a short instructions blurb.
- **Bots** receive server-issued Identities from the **Server**.

## 2. Coordinate in a Channel

Work is discussed in a **Channel**. **Activity** in the Channel is the timeline of what happened: messages, **Handoffs**, **Runs**, and **Decision** records. `@Scout` / `@Builder` (or `@Role`) mentions create a Task + Handoff to that Bot.

## 3. Create a Task

A **Member** opens a **Task**. By default the Server auto-Handoffs it to **Scout** (`?handoff=scout|builder|none`). The Task has an owner and optional links to a **Repo**, **Issues**, or **Pipelines**.

## 4. Hand off to a Bot

A **Handoff** targets a Member or a Role (`to_role=builder`). Completing a Scout Handoff on an ambiguous Task (title contains `?`, or notes say unclear/TBD) opens a **Decision** stub. A Handoff to **Builder** can enqueue a **Run** when `?autorun=1` or the Project was created with `auto_run: true` (default off).

## 5. Execute a Run

The Bot starts a **Run** on a **Worker**. With `--acp` the Worker runs an ACP agent CLI (`claude` / `codex` / `opencode` / `goose`); otherwise it runs a command in a Docker **Sandbox**. Tokens, tool calls and logs stream to the Server as RunEvents (`GET /v1/runs/{id}/events`, WebSocket `/v1/runs/{id}/ws`). The rolled-up transcript is stored as Artifact `acp.log` or `sandbox.log`. An agent that is not installed fails the Run; **FakeACP** runs only when agent `fake` is requested, for tests and demos.

A Run may produce Artifacts (patches, logs, reports). Those Artifacts stay attached to the Task.

## 6. Record a Decision

When the team commits to a choice, a **Member** writes a **Decision**. The **Decisions** surface is the durable list for the Project — not chat scrollback. After an answer is stored, the same normalized question is auto-applied from **Decision memory** (Activity notes reuse; no new inbox item). New Decisions, Handoffs to you, Channel `@bot` Tasks, and Pipeline failures also land in the Member **Notification** inbox.

## 7. Repeat with Routines

A **Routine** can reopen this loop on a schedule or a trigger (for example, a new Issue or a failed Pipeline). The same nouns apply: Task, Handoff, Run, Artifact, Decision.

## Not built yet

See the [roadmap](roadmap.md). Notably: Runs queued and pulled by Workers, per-Run containers for ACP agents, bidirectional ACP (steering, permission requests as Decisions), agent-written diffs becoming PRs, and signed Activity.

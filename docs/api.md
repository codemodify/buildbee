# HTTP API

Everything is JSON under `/v1`. The Server also serves the web UI and `/healthz`.

## Who is acting

There is no login. Each request resolves an actor once:

| Source | Used by | Actor |
| --- | --- | --- |
| `buildbee_person` cookie (set by `POST /v1/me`) | the web UI | that Person |
| `X-BuildBee-As: <name>` | CLI, scripts | the Person with that name (created on first use) |
| `X-BuildBee-Worker: <name>` | workers | the worker; its Run changes are attributed to the Run's Bot |
| none | anyone | anonymous: may read; actions that need a Person answer 401 |

A Person who writes to a Project they are not in joins it as `member`. Activity entries name the actor (`actor`, `actor_member_id`).

```http
POST /v1/me            {"name": "Ada"}   → sets the cookie, returns {"person": {...}}
GET  /v1/me                              → {"person": {...} | null}
PATCH /v1/me           {"name": "Ada L"} → rename yourself everywhere (409 if the name is taken)
DELETE /v1/me                            → forget this browser's Person
```

## Resources

| Method and path | What it does |
| --- | --- |
| `GET/POST /v1/projects` | list (`?archived=1` includes archived) / create `{name, auto_run}`: you and `#tasks`, no Bots. `default_bots: true` also adds Scout, Builder, Sentry and Pulse, running `agent` (`""` = any) |
| `GET /v1/bot-templates` | the autopilot's Bots (`name`, `role`, `instructions`), to add with `POST /v1/projects/{id}/members` |
| `GET/PATCH /v1/projects/{id}` | Project with Members and Channels / `{name, auto_run, merge_policy: auto\|approval, max_runs, instructions, repo_url, default_branch, agent_image, archived}`; `auto_run` is [autopilot](autopilot.md); `agent_image` is the container image its agents run in (`""` = the worker's); `agent_permissions` is `auto` (agents act freely in their Run) or `ask` (each permission request is a Decision) |
| `POST /v1/runs/{id}/permission` | worker only, for its running Run: `{title, options: [{id, name, kind}]}` → a Decision (`action: permission`, `run_id`) on the Task, recommending the allow-once option. The worker polls `GET /v1/decisions/{id}` and gives the agent the chosen option; a Run that ends closes its open ones |
| `GET /v1/decisions/{id}` | one Decision |
| `POST /v1/projects/{id}/join`, `POST /v1/projects/{id}/leave` | join as `member` / leave (messages keep your name; writing again rejoins). Both show in `GET /v1/members` |
| `GET/POST /v1/projects/{id}/members` | list / add `{kind: human\|bot, display_name, role, instructions, agent}` |
| `PATCH /v1/members/{id}` | `{display_name, instructions, agent}`; `agent` applies to Bots |
| `DELETE /v1/members/{id}` | remove a person or Bot from its Project (`left_at`); messages keep the name. A removed Bot's queued and running Runs are canceled and it takes no more work; a removed person comes back by writing |
| `GET/POST /v1/projects/{id}/channels`, `PATCH /v1/channels/{id}` | Channels; `{name, archived}`. Every Project starts with `#tasks` (`locked`: first, cannot be renamed or archived), where every Task's thread lives |
| `GET/POST /v1/channels/{id}/messages` | page root messages (each with `reply_count`, `task_id`) / post `{body}`; `@Bot` opens a Task handed to it, with this message as its thread, and starts the Bot's Run; `@Person` notifies them |
| `GET /v1/messages/{id}/thread`, `POST /v1/messages/{id}/replies` | a thread `{root, replies, has_more}` / reply `{body, file_ids}`; messages carry their `files`. In a Task's thread, `@Bot` hands that Task on, and any other reply reaches the agent working on it |
| `PATCH /v1/messages/{id}`, `DELETE /v1/messages/{id}` | edit `{body}` / delete your own message (409 for anyone else's). An edit sets `edited_at` and starts nothing; a delete empties `body`, sets `deleted_at` and keeps the thread. Both reach viewers live as `message_edited` / `message_deleted` on `channel:<id>` (no cursor: not replayed) |
| `GET/POST /v1/dms` | DMs are between people. Every DM you are in and have not closed, newest first, with `with` (the others) and `unread` / open one with `{person_ids}`: one conversation per set of people, whichever Projects they share. Writing in a DM notifies the others in it; Bots are asked in channels |
| `DELETE /v1/dms/{id}` | delete a DM and its messages for everyone in it; the others are notified |
| `POST /v1/dms/{id}/close` | take a DM out of your list; it comes back with a new message or when you open it again. Nothing is deleted |
| `GET /v1/projects/{id}/unread`, `POST /v1/channels/{id}/read` | per Channel and DM: `{channel_id, unread, last_seq}` / mark read up to `{seq}` |
| `GET /v1/members` | everyone on the Server: `people` with their Projects (`left_at` if they left), `bots` with `project_name`, and recent `events` (created, joined, left, added), newest first |
| `GET /v1/runs` | what the agents are doing across open Projects: Runs running, then queued, then those that failed in the last day, each with `task_title`, `project_name` and `bot_name` |
| `GET /v1/search?q=&project_id=` | messages you can read (open channels of open Projects, your DMs) with every word of `q` as a word or prefix, best first, up to 50; each with `channel_name`, `channel_kind`, `project_name` and `author_name` |
| `GET /v1/presence` | people with the app open; workers seen in the last 90 s with their agents, `slots` and `running`; `agents` (each agent's `workers`, `running`, `queued`; `any` is Runs that take whichever); `local`, the Server's own worker (`state` off, starting, running or unavailable, with `reason` and `isolation`) |
| `GET /v1/projects/{id}/activity` | page of the Project log, newest first (`?type=`) |
| `GET/POST /v1/projects/{id}/tasks` | list / create `{title, body, assignee_member_id, handoff_role, handoff_note}`; `handoff_role` defaults to `scout`, `none` skips |
| `GET/PATCH /v1/tasks/{id}` | Task / `{title, body, status}` with status `open\|in_progress\|done\|canceled` |
| `GET /v1/tasks/{id}/detail` | Task with Handoffs, Runs, Artifacts (no bodies) and Pipelines |
| `GET/POST /v1/tasks/{id}/handoffs` | list / hand off `{to_member_id \| to_role, note, autorun}` |
| `POST /v1/handoffs/{id}/complete` | complete once (`409` after); `?decision=1` forces the Scout Decision |
| `GET/POST /v1/projects/{id}/decisions` | list (`?open=1`, `?mine=1`) / ask `{prompt, options, recommendation, assignee_id, task_id}` |
| `POST /v1/decisions/{id}/answer` | answer once `{answer}`; remembered for the same question |
| `GET /v1/projects/{id}/decisions/memories` | remembered answers |
| `GET/POST /v1/tasks/{id}/runs` | list / queue `{agent, bot_member_id, kind: plan\|build\|review}`; all default from the Task's Bot |
| `GET/PATCH /v1/runs/{id}` | Run / `{status, detail}`: `pending → running → succeeded\|failed\|canceled`; people may only cancel; workers also report `{summary, branch, pr_url}` |
| `POST /v1/runs/{id}/steer` | people: `{text, interrupt}`; a message for the agent, folded into a queued Run's prompt or delivered as the running agent's next turn (`interrupt` cancels the current turn first) |
| `GET/POST /v1/runs/{id}/events` | `?after=<seq>&limit=` / append `{kind, payload}` (`409` once finished) |
| `POST /v1/worker/claim` | workers: `{agents, wait_seconds ≤ 30}` → `200 {run, task, project, bot, handoffs}` or `204` |
| `POST /v1/runs/{id}/heartbeat` | workers: renew the 60 s lease; returns the Run (status `canceled` means stop) |
| `GET/POST /v1/tasks/{id}/artifacts`, `GET /v1/artifacts/{id}` | listings carry `size`; fetch one for its `body`. Bodies over 64 KiB live in the blob store: then `body` is the first MiB, with `truncated` and `raw_url` |
| `GET /v1/artifacts/{id}/raw` | all of an Artifact's body, as text |
| `POST /v1/channels/{id}/files?name=` | upload one file as the raw body (Content-Length required, up to `BUILDBEE_MAX_UPLOAD_BYTES`) → `{id, name, content_type, size, sha256, url}`. Its type is sniffed from its bytes. Attach it by passing `file_ids` when posting a message or reply there; unposted uploads are yours alone and are deleted after a day |
| `GET /v1/files/{id}` | download an attachment you can read. PNG, JPEG, GIF and WebP are served inline; everything else as `application/octet-stream` with `Content-Disposition: attachment`, `nosniff` and a sandboxing CSP. Deleting the message deletes its files |
| `GET /v1/usage?days=30`, `GET /v1/projects/{id}/usage?days=30` | Runs, cost (by currency) and context tokens, by agent and by Project, as agents reported them |
| `GET/POST /v1/tasks/{id}/pipelines`, `PATCH /v1/pipelines/{id}` | CI checks; a failure notifies every Person |
| `POST /v1/projects/{id}/issues/sync` | import open Issues as Tasks (`503` without GitHub) |
| `GET/POST /v1/projects/{id}/routines`, `PATCH /v1/routines/{id}`, `POST /v1/routines/{id}/run` | Routines `{name, prompt, schedule, enabled, bot_member_id}`: each firing opens a Task with `prompt` for the Bot ([autopilot.md](autopilot.md#routines)); fire now |
| `GET /v1/me/notifications`, `POST /v1/me/notifications/read-all`, `POST /v1/notifications/{id}/read` | the acting Person's inbox across Projects |
| `GET/PATCH /v1/me/preferences` | `{mute_mentions, mute_routines}` |
| `POST /v1/pipelines/webhook`, `POST /v1/issues/webhook?project_id=` | GitHub webhooks; signed when `GITHUB_WEBHOOK_SECRET` is set. CI results take `{task_id \| branch, name, status, commit, external_url}` or a GitHub `check_run` event, matched to the Task by branch and to the push by `head_sha` |

## Paging

Messages, Activity and notifications page by `seq`: `?limit=` (default 100, max 1000), `?before=<seq>` for older, `?after=<seq>` for newer. Responses are `{"items": [...], "has_more": bool}`. Messages read oldest-to-newest; Activity and notifications newest first. Run events page with `?after=<seq>`.

## Errors

`{"error": "..."}` with `400` invalid input, `401` needs a Person, `404` not found, `409` conflicts with the current state (archived Project, finished Run, second answer), `413` body too large, `503` integration not configured. Unexpected errors are `500 {"error": "internal error"}`; the cause is in the Server log.

## Live events

`GET /v1/ws` is one socket for many topics. Send:

```json
{"op": "subscribe", "topic": "run:<id>", "after": 12}
{"op": "unsubscribe", "topic": "run:<id>"}
```

Topics: `project:<id>` (Activity), `channel:<id>` (messages and thread replies; a reply has `thread_id`), `run:<id>` (RunEvents), `person:<id>` (notifications), `presence:server` (live only: someone came or went; fetch `/v1/presence`). An open `/v1/ws` also marks its Person online. With `after`, missed events are replayed from the database before live ones, with no gap and no duplicates; without it only live events flow. Frames are `{"topic", "cursor", "type", "data"}`.

`GET /v1/runs/{id}/ws` and `GET /v1/channels/{id}/ws` stream one topic with bare `data` frames. A Run stream replays its whole transcript unless `?after=` is given.

A client that falls too far behind is disconnected; reconnect with the last cursor you saw. WebSockets only accept pages from the Server's own origin.

## Workers

Runs are a queue. A worker claims a Run, owns it until it finishes, and must heartbeat to keep it; only the claiming worker may append events, change its status or attach Artifacts to it (`409` otherwise). A Run whose worker stops heartbeating is failed by the Server. See [workers.md](workers.md).

Runs have a `kind`: `plan` (Scout), `build` (Builder), `review` (Sentry, with a `verdict` of `approve` or `changes`) or `merge` (no agent). Tasks record the `branch` and `pr_url` their Builder pushed, and `merged_at`. Decisions with `action: "merge"` merge the Task's branch when answered `merge`. See [autopilot.md](autopilot.md).

## Metrics

`GET /metrics` answers in Prometheus text format, without a login like the rest of a LAN Server: Runs by status, Runs running and queued per agent, workers online and their slots, people online, and HTTP requests by method and status with a duration histogram (long-polls and WebSockets are counted but not timed).

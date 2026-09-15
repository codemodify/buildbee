# Autopilot

With autopilot on (`PATCH /v1/projects/{id}` `{"auto_run": true}`), a Project's Bots move each Task from idea to merged code by themselves. People watch, answer Decisions, and step in whenever they like.

```
Task ─▶ Scout plans ─▶ Builder builds and pushes ─▶ Sentry reviews ─┬─ approves ─▶ CI passes ─▶ merge
                              ▲                                     │
                              └──────── changes requested ◀─────────┤
                              └──────── CI failed ◀─────────────────┘
```

Each step is an ordinary Handoff and Run, recorded in Activity, visible in the Task's detail and streamed live.

## The steps

| Step | Run kind | What the agent is told | What happens next |
| --- | --- | --- | --- |
| Scout | `plan` | read the Task and the repo, change nothing, reply with a plan | the plan is handed to the Builder |
| Builder | `build` | implement the Task, run the checks, summarize | the worker commits and pushes the branch (and opens a PR on GitHub); the Task records `branch` and `pr_url`; Sentry is asked to review |
| Sentry | `review` | review the branch against the default branch, change nothing, end with `VERDICT: APPROVE` or `VERDICT: REQUEST_CHANGES` | approve: merge once CI agrees; changes: back to the Builder with the review |
| merge | `merge` | (no agent) | a worker merges: `gh pr merge --squash --delete-branch` for a GitHub PR, otherwise a `git merge --no-ff` into the default branch; the Task is done |

Every prompt starts with the Project's `instructions` (standing guidance for all agents: conventions, commands, what not to touch) and the Bot's own instructions. Later Builder rounds continue the same branch and get the latest review or CI failure as their Handoff note.

## Merging

- `merge_policy: "auto"` (the default) merges as soon as Sentry approves the latest build and CI passes.
- `merge_policy: "approval"` asks the Project's people instead, with a Decision whose `action` is `merge`: answering `merge` merges; anything else leaves the Task as it is.

CI counts only when it reports on the Task's latest push, through `POST /v1/pipelines/webhook`. GitHub `check_run` events are matched to the Task by their branch. A Task with no CI reports merges on Sentry's approval alone. A failed check goes back to the Builder with the check's name and link; a pending one holds the merge until it finishes.

## Routines

A Routine opens a Task on a schedule and hands it to a Bot, which starts working at once: "update dependencies" every week, "look for flaky tests" every night.

```bash
buildbee routine create --project ID --name "Update dependencies" --schedule 168h --enabled \
  --prompt "Update dependencies to their latest compatible versions, fix what breaks, keep the change small."
```

The Task goes to Scout unless the Routine names another Bot (`bot_member_id`, Scout, Builder or Sentry). Pulse posts a line in the first channel and people get a notification (muted with `mute_routines`). While the Task a Routine opened last is still open, the Routine skips its turn, so work never piles up.

## Limits

- The Builder gets 3 attempts per Task. After the third, a person decides with a `merge` Decision (recommended answer: stop).
- A Run that fails stops the Task and notifies people; nothing is retried by itself. Queue a Run, or hand the Task on, to continue.
- A review with no verdict line stops the Task and notifies people.
- Without autopilot, Runs still record their branch, PR and verdict, but nothing follows by itself.

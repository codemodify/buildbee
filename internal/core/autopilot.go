package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
)

// Autopilot moves a Task through a Project's Bots without people, when the
// Project's auto_run is on:
//
//	Scout plans → Builder builds and pushes → Sentry reviews →
//	merge (after CI passes; or a person approves it, per merge_policy)
//
// Sentry asking for changes, or CI failing, sends the Task back to the
// Builder on the same branch. After maxRounds builds a person decides.
// Every step is an ordinary Handoff and Run, recorded in Activity.

// maxRounds caps the Builder's attempts on one Task before a person decides.
const maxRounds = 3

// advance runs after r finished, in the same transaction.
func (w *work) advance(ctx context.Context, r *models.Run) error {
	task, err := w.st.GetTask(ctx, r.TaskID, true)
	if err != nil {
		return err
	}
	proj, err := w.st.GetProject(ctx, r.ProjectID)
	if err != nil {
		return err
	}
	speaker := w.botOf(ctx, r)
	if r.Status != models.RunSucceeded {
		if r.Status == models.RunFailed {
			if err := w.note(ctx, task, speaker, "Failed: "+r.Detail); err != nil {
				return err
			}
			return w.notifyPeople(ctx, proj.ID, nil, notice{projectID: proj.ID, kind: "run",
				title: fmt.Sprintf("%s Run failed on %s", titleCase(string(r.Kind)), task.Title), body: r.Detail,
				href: href(proj.ID, "tasks", task.ID)})
		}
		return nil
	}
	if err := w.closeHandoffsTo(ctx, task.ID, r.BotMemberID); err != nil {
		return err
	}
	if err := w.note(ctx, task, speaker, resultNote(r)); err != nil {
		return err
	}
	if r.Kind == models.RunMerge {
		t := w.now
		task.Status, task.MergedAt, task.UpdatedAt = models.TaskDone, &t, w.now
		if err := w.st.UpdateTask(ctx, *task); err != nil {
			return err
		}
		if err := w.activity(ctx, proj.ID, who{name: "autopilot"}, models.TypeTask, "merged", task.ID,
			map[string]any{"branch": task.Branch, "pr_url": task.PRURL}); err != nil {
			return err
		}
		return w.notifyPeople(ctx, proj.ID, nil, notice{projectID: proj.ID, kind: "merged",
			title: "Merged: " + task.Title, body: firstNonEmpty(task.PRURL, task.Branch), href: href(proj.ID, "tasks", task.ID)})
	}
	if r.Kind == models.RunBuild && r.Branch != "" && r.Branch != proj.DefaultBranch {
		task.Branch, task.UpdatedAt = r.Branch, w.now
		if r.Commit != "" {
			task.HeadCommit = r.Commit
		}
		if r.PRURL != "" {
			task.PRURL = r.PRURL
		}
		if err := w.st.UpdateTask(ctx, *task); err != nil {
			return err
		}
	}
	if !proj.AutoRun || task.Status == models.TaskDone || task.Status == models.TaskCanceled {
		return nil
	}
	switch r.Kind {
	case models.RunPlan:
		return w.passOn(ctx, proj, task, models.RoleScout, models.RoleBuilder, "Scout's plan:\n\n"+r.Summary)
	case models.RunBuild:
		if task.Branch == "" {
			return w.notifyPeople(ctx, proj.ID, nil, notice{projectID: proj.ID, kind: "run",
				title: "Builder changed nothing for " + task.Title, body: truncate(r.Summary, 500), href: href(proj.ID, "tasks", task.ID)})
		}
		return w.passOn(ctx, proj, task, models.RoleBuilder, models.RoleSentry,
			"Please review branch "+task.Branch+". The Builder's summary:\n\n"+r.Summary)
	case models.RunReview:
		switch r.Verdict {
		case "approve":
			return w.tryMerge(ctx, proj, task)
		case "changes":
			return w.sendBack(ctx, proj, task, models.RoleSentry, "Sentry asked for changes:\n\n"+r.Summary)
		default:
			return w.notifyPeople(ctx, proj.ID, nil, notice{projectID: proj.ID, kind: "run",
				title: "Sentry's review of " + task.Title + " has no verdict", body: truncate(r.Summary, 500),
				href: href(proj.ID, "tasks", task.ID)})
		}
	}
	return nil
}

// passOn hands the Task from the Bot with role from to the Bot with role
// to, which starts a Run. A Project without such a Bot stops here.
func (w *work) passOn(ctx context.Context, proj *models.Project, task *models.Task, fromRole, toRole, note string) error {
	members, err := w.st.ListMembers(ctx, proj.ID)
	if err != nil {
		return err
	}
	from, to := models.MemberByRole(members, fromRole), models.MemberByRole(members, toRole)
	if from == nil || to == nil {
		return nil
	}
	_, _, err = w.handoff(ctx, proj, task, from, to, System("autopilot"), truncate(note, 4000), true)
	return err
}

// sendBack returns the Task to the Builder, or asks people once the
// Builder has had maxRounds attempts.
func (w *work) sendBack(ctx context.Context, proj *models.Project, task *models.Task, fromRole, why string) error {
	runs, err := w.st.ListRuns(ctx, task.ID)
	if err != nil {
		return err
	}
	builds := 0
	for _, r := range runs {
		if r.Kind == models.RunBuild {
			if !r.Status.Terminal() {
				return nil // the Builder is already on it
			}
			if r.Status == models.RunSucceeded {
				builds++
			}
		}
	}
	if builds < maxRounds {
		return w.passOn(ctx, proj, task, fromRole, models.RoleBuilder, why)
	}
	return w.askToMerge(ctx, proj, task, newDecision{
		prompt: fmt.Sprintf("The Builder has tried %d times on \"%s\" (%s) and it is still not ready: %s. Merge it anyway?",
			builds, task.Title, firstNonEmpty(task.PRURL, task.Branch), truncate(firstLine(why), 200)),
		options: []string{"merge", "stop"}, recommendation: "stop",
	})
}

// askToMerge opens a merge Decision about the Task's head commit, unless
// one is already waiting for an answer.
func (w *work) askToMerge(ctx context.Context, proj *models.Project, task *models.Task, d newDecision) error {
	ds, err := w.st.ListDecisions(ctx, proj.ID)
	if err != nil {
		return err
	}
	for _, x := range ds {
		if x.TaskID == task.ID && x.Action == "merge" && x.Answer == "" {
			return nil
		}
	}
	d.taskID, d.action, d.commit = task.ID, "merge", task.HeadCommit
	_, err = w.openDecision(ctx, proj.ID, who{name: "autopilot"}, nil, d)
	return err
}

// tryMerge merges an approved Task once CI agrees: at once with the
// auto merge policy, after a person says so with approval.
func (w *work) tryMerge(ctx context.Context, proj *models.Project, task *models.Task) error {
	if task.MergedAt != nil || task.Branch == "" {
		return nil
	}
	runs, err := w.st.ListRuns(ctx, task.ID) // newest first
	if err != nil {
		return err
	}
	var lastBuild, lastReview *models.Run
	for i := range runs {
		r := &runs[i]
		if r.Kind == models.RunMerge && !r.Status.Terminal() {
			return nil // already merging
		}
		if r.Status != models.RunSucceeded {
			continue
		}
		if r.Kind == models.RunBuild && lastBuild == nil {
			lastBuild = r
		}
		if r.Kind == models.RunReview && lastReview == nil {
			lastReview = r
		}
	}
	if lastReview == nil || lastReview.Verdict != "approve" || !reviewed(lastReview, lastBuild, task) {
		return nil // the latest work is not approved
	}
	ci, detail, err := w.ciState(ctx, task, lastBuild)
	if err != nil {
		return err
	}
	switch ci {
	case models.PipelineFailure:
		return w.sendBack(ctx, proj, task, models.RoleSentry, "CI failed: "+detail)
	case models.PipelinePending:
		return w.activity(ctx, proj.ID, who{name: "autopilot"}, models.TypeTask, "waiting_for_ci", task.ID, map[string]any{"checks": detail})
	}
	if proj.MergePolicy == models.MergeApproval {
		return w.askToMerge(ctx, proj, task, newDecision{
			prompt: fmt.Sprintf("Sentry approved \"%s\" (%s, commit %.12s) and CI passed. Merge it?",
				task.Title, firstNonEmpty(task.PRURL, task.Branch), task.HeadCommit),
			options: []string{"merge", "not yet"}, recommendation: "merge",
		})
	}
	return w.queueMerge(ctx, task, who{name: "autopilot"})
}

// reviewed reports whether review covers the Task's latest work: the very
// commit when the worker reported it, else a review that started after the
// last build.
func reviewed(review, build *models.Run, task *models.Task) bool {
	if review.Commit != "" && task.HeadCommit != "" {
		return review.Commit == task.HeadCommit
	}
	return build == nil || !review.CreatedAt.Before(build.CreatedAt)
}

// ciState sums up the Task's CI checks reported since build finished:
// failure if any failed, pending if any is still running or none has
// reported yet on a Task that has CI, else success.
func (w *work) ciState(ctx context.Context, task *models.Task, build *models.Run) (models.PipelineStatus, string, error) {
	ps, err := w.st.ListPipelines(ctx, task.ID) // newest first
	if err != nil {
		return "", "", err
	}
	if len(ps) == 0 {
		return models.PipelineSuccess, "no CI", nil
	}
	latest := map[string]models.Pipeline{}
	for _, p := range ps {
		switch {
		case p.Commit != "" && task.HeadCommit != "":
			if !strings.EqualFold(p.Commit, task.HeadCommit) {
				continue // about another push
			}
		case build != nil && build.StartedAt != nil && p.UpdatedAt.Before(*build.StartedAt):
			continue // reported without a commit, before the latest build began
		}
		if cur, ok := latest[p.Name]; !ok || p.UpdatedAt.After(cur.UpdatedAt) {
			latest[p.Name] = p
		}
	}
	if len(latest) == 0 {
		return models.PipelinePending, "waiting for CI on the latest push", nil
	}
	state, names := models.PipelineSuccess, []string{}
	for name, p := range latest {
		switch p.Status {
		case models.PipelineFailure:
			return models.PipelineFailure, strings.TrimSpace(name + " " + p.ExternalURL), nil
		case models.PipelinePending:
			state = models.PipelinePending
			names = append(names, name)
		}
	}
	return state, strings.Join(names, ", "), nil
}

// queueMerge queues a merge Run of the Task's head commit, which any
// worker executes; the merge fails if the branch has moved since.
func (w *work) queueMerge(ctx context.Context, task *models.Task, by who) error {
	r := models.Run{ID: uuid.NewString(), TaskID: task.ID, ProjectID: task.ProjectID, Kind: models.RunMerge,
		Status: models.RunPending, Detail: "queued", Commit: task.HeadCommit, CreatedAt: w.now, UpdatedAt: w.now}
	if err := w.st.InsertRun(ctx, r); err != nil {
		return err
	}
	w.queued = true
	if err := w.runEvent(ctx, r.ID, models.RunEventStatus, map[string]any{"status": r.Status, "detail": r.Detail}); err != nil {
		return err
	}
	return w.activity(ctx, task.ProjectID, by, models.TypeRun, "created", r.ID,
		map[string]any{"task_id": task.ID, "kind": r.Kind, "branch": task.Branch, "commit": r.Commit})
}

// closeHandoffsTo completes the Task's open Handoffs to the Bot that just
// finished a Run on it.
func (w *work) closeHandoffsTo(ctx context.Context, taskID, memberID string) error {
	if memberID == "" {
		return nil
	}
	hs, err := w.st.ListHandoffs(ctx, taskID)
	if err != nil {
		return err
	}
	for _, h := range hs {
		if h.ToMemberID == memberID && h.Status == models.HandoffOpen {
			if _, err := w.st.CompleteHandoff(ctx, h.ID, w.now); err != nil {
				return err
			}
		}
	}
	return nil
}

// onPipeline reacts to a CI result on a Task the Builder pushed: a failure
// goes back to the Builder, a pass may complete an approved merge.
func (w *work) onPipeline(ctx context.Context, task *models.Task, p *models.Pipeline) error {
	if task.Branch == "" || task.MergedAt != nil || task.Status == models.TaskDone || task.Status == models.TaskCanceled {
		return nil
	}
	proj, err := w.st.GetProject(ctx, task.ProjectID)
	if err != nil || !proj.AutoRun {
		return err
	}
	if p.Status == models.PipelineFailure {
		return w.sendBack(ctx, proj, task, models.RoleSentry, "CI failed: "+strings.TrimSpace(p.Name+" "+p.ExternalURL))
	}
	if p.Status == models.PipelineSuccess {
		return w.tryMerge(ctx, proj, task)
	}
	return nil
}

// onDecision acts on answered Decisions that carry an action.
func (w *work) onDecision(ctx context.Context, d *models.Decision, by who) error {
	if d.Action != "merge" || !strings.EqualFold(strings.TrimSpace(d.Answer), "merge") || d.TaskID == "" {
		return nil
	}
	task, err := w.st.GetTask(ctx, d.TaskID, true)
	if err != nil {
		return err
	}
	if task.MergedAt != nil || task.Branch == "" || task.Status == models.TaskCanceled || task.Status == models.TaskDone {
		return nil
	}
	if d.Commit != "" && d.Commit != task.HeadCommit {
		// New work was pushed after the question was asked: that work was
		// not what people agreed to merge.
		return w.notifyPeople(ctx, task.ProjectID, nil, notice{projectID: task.ProjectID, kind: "run",
			title: "Not merged: " + task.Title + " changed after the merge was approved",
			body:  "Approved " + shortSHA(d.Commit) + ", now " + shortSHA(task.HeadCommit), href: href(task.ProjectID, "tasks", task.ID)})
	}
	runs, err := w.st.ListRuns(ctx, task.ID)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.Kind == models.RunMerge && !r.Status.Terminal() {
			return nil
		}
	}
	return w.queueMerge(ctx, task, by)
}

// runPrompt is what the agent is asked to do: the Project's guidance, the
// Bot's standing instructions, what this kind of Run is for, the thread it
// was asked in (convo, which carries the ask and what everyone including
// the other Bots has said), and the latest notes handed to the Bot.
func runPrompt(p *models.Project, bot *models.Member, task *models.Task, notes []models.Handoff, convo string,
	bw botWork, kind models.RunKind) string {
	var b strings.Builder
	if p != nil && p.Instructions != "" {
		b.WriteString("Project guidance:\n" + p.Instructions + "\n\n")
	}
	if bot != nil && bot.Instructions != "" {
		b.WriteString(bot.Instructions + "\n\n")
	}
	base := "the default branch"
	if p != nil && p.DefaultBranch != "" {
		base = "origin/" + p.DefaultBranch
	}
	switch kind {
	case models.RunPlan:
		b.WriteString("You are planning, not changing code. Read the Task and the repository in your working directory, " +
			"but do not modify any file. Reply with a short plan for the Builder: what to change, where, and how to check it. " +
			"If the Task is unclear, say exactly what is missing.\n\n")
	case models.RunBuild:
		b.WriteString("Implement the Task in the repository in your working directory. Keep the change focused and " +
			"run the project's tests or checks if it has any. You may commit; anything left uncommitted is committed " +
			"for you. Do not push: BuildBee pushes the branch. End with a short summary of what you changed and how you checked it.\n\n")
		if bw.mine != "" {
			b.WriteString("You are continuing your branch " + bw.mine + "; your earlier work is already checked out.\n\n")
		}
	case models.RunReview:
		b.WriteString("You are reviewing the Builder's work on branch " + bw.mine + ", which is checked out in your " +
			"working directory. Compare it with " + base + " (for example `git diff " + base + "...HEAD`). Check correctness, " +
			"tests and security. Do not modify any file. End your reply with one line that is exactly " +
			"`VERDICT: APPROVE` or `VERDICT: REQUEST_CHANGES`, after your reasons.\n\n")
	}
	b.WriteString("Asked: " + task.Title + "\n\n")
	// Say nothing twice: when a person asked in chat, the thread below
	// already carries the words they used.
	if task.Body != "" && !strings.Contains(convo, task.Body) {
		b.WriteString(task.Body + "\n\n")
	}
	b.WriteString(convo)
	var mine []models.Handoff
	for _, h := range notes {
		// A note that is already in the thread is not worth saying twice.
		if h.Note != "" && (bot == nil || h.ToMemberID == bot.ID) && !strings.Contains(convo, h.Note) {
			mine = append(mine, h)
		}
	}
	if len(mine) > 3 {
		mine = mine[len(mine)-3:]
	}
	for _, h := range mine {
		b.WriteString("\nHandoff note: " + h.Note + "\n")
	}
	// Other Bots asked the same thing work on branches of their own. Naming
	// them lets a Bot read their work (`git diff origin/<branch>`) and say
	// what it thinks of it: they share the repo, so nothing needs fetching.
	if len(bw.others) > 0 && kind != models.RunMerge {
		b.WriteString("\nOther Bots are on this too, each on its own branch:\n")
		for _, o := range bw.others {
			b.WriteString("- " + o + "\n")
		}
		b.WriteString("Read their work with `git diff " + base + "...origin/<branch>` before you finish, " +
			"and say plainly where you agree, where you differ and why yours is better.\n")
	}
	return b.String()
}

func shortSHA(c string) string { return c[:min(12, len(c))] }

// resultNote is what the Bot says in the thread when its Run succeeds: the
// answer, and what it pushed when it changed the repo. A Run that changed
// nothing says only its answer — there is nothing to report.
func resultNote(r *models.Run) string {
	switch r.Kind {
	case models.RunBuild:
		if r.Branch == "" {
			return r.Summary
		}
		head := "Pushed " + r.Branch
		if r.Commit != "" {
			head += " at " + shortSHA(r.Commit)
		}
		if r.PRURL != "" {
			head += " (" + r.PRURL + ")"
		}
		return head + ".\n\n" + r.Summary
	case models.RunMerge:
		return "Merged " + strings.TrimPrefix(r.Detail, "merged ") + "."
	default:
		return r.Summary
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

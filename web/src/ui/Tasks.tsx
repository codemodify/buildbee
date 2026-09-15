import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { go } from "../route";
import { ago, DiffLines, Markdown } from "../text";
import type { Artifact, Task } from "../types";
import { useCtx } from "./context";
import { Avatar, Button, ErrorNote, Field, Pill, Sheet, cx, inputClass, runTone, taskLabel, taskTone } from "./kit";
import { RunPanel } from "./RunPanel";
import { useTaskDetail } from "./Thread";

const columns: { status: string; title: string }[] = [
  { status: "open", title: "Open" },
  { status: "in_progress", title: "In progress" },
  { status: "done", title: "Done" },
];

/** Board shows a Project's Tasks by status. */
export function Board({ header }: { header: ReactNode }) {
  const { data } = useCtx();
  const [creating, setCreating] = useState(false);
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="flex items-center justify-between px-4 pt-3">
        <p className="text-[13px] text-bb-subtle">
          {data.project.auto_run ? "Autopilot is on: Bots take each Task from plan to merge." : "Autopilot is off: hand Tasks to Bots yourself."}
        </p>
        <Button tone="primary" size="sm" onClick={() => setCreating(true)}>
          New Task
        </Button>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 overflow-y-auto p-4 md:grid-cols-3">
        {columns.map((c) => {
          const tasks = data.tasks.filter((t) => t.status === c.status);
          return (
            <div key={c.status} className="min-w-0">
              <p className="mb-2 flex items-center gap-2 text-[12px] font-semibold tracking-wide text-bb-subtle uppercase">
                {c.title} <span className="font-normal">{tasks.length}</span>
              </p>
              <div className="space-y-2">
                {tasks.map((t) => (
                  <TaskTile key={t.id} task={t} />
                ))}
                {tasks.length === 0 && <p className="rounded-lg border border-dashed border-bb-border px-3 py-6 text-center text-[12.5px] text-bb-subtle">Nothing here</p>}
              </div>
            </div>
          );
        })}
      </div>
      {creating && <NewTask onClose={() => setCreating(false)} />}
    </section>
  );
}

function TaskTile({ task }: { task: Task }) {
  const { members, data } = useCtx();
  const assignee = task.assignee_member_id ? members.get(task.assignee_member_id) : undefined;
  return (
    <button
      type="button"
      onClick={() => go({ view: "task", projectId: data.project.id, taskId: task.id })}
      className="block w-full rounded-lg border border-bb-border bg-bb-surface p-3 text-left shadow-sm hover:border-bb-accent-strong/60"
    >
      <p className="text-[13.5px] leading-snug font-medium">{task.title}</p>
      <div className="mt-2 flex items-center gap-2 text-[12px] text-bb-subtle">
        {assignee && <Avatar member={assignee} size={18} />}
        <span className="truncate">{assignee?.display_name ?? "Unassigned"}</span>
        {task.merged_at && <Pill tone="success">Merged</Pill>}
        {task.pr_url && !task.merged_at && <Pill tone="accent">PR</Pill>}
        <span className="ml-auto shrink-0">{ago(task.updated_at)}</span>
      </div>
    </button>
  );
}

function NewTask({ onClose }: { onClose: () => void }) {
  const { data, reload } = useCtx();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [role, setRole] = useState(data.project.auto_run ? "scout" : "builder");
  const [err, setErr] = useState("");
  return (
    <Sheet title="New Task" onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            const t = await api.createTask(data.project.id, { title: title.trim(), body, handoff_role: role, autorun: role !== "none" });
            reload();
            onClose();
            go({ view: "task", projectId: data.project.id, taskId: t.id });
          } catch (e2) {
            setErr(e2 instanceof Error ? e2.message : String(e2));
          }
        }}
      >
        <Field label="What needs doing">
          <input className={inputClass} autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Add rate limiting to /login" />
        </Field>
        <Field label="Details" hint="Context, acceptance criteria, links. Markdown works.">
          <textarea className={cx(inputClass, "min-h-28")} value={body} onChange={(e) => setBody(e.target.value)} />
        </Field>
        <Field label="Start with">
          <select className={inputClass} value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="scout">Scout plans it first</option>
            <option value="builder">Builder starts building</option>
            <option value="none">Nobody yet</option>
          </select>
        </Field>
        <ErrorNote>{err}</ErrorNote>
        <Button tone="primary" type="submit" disabled={!title.trim()}>
          Create Task
        </Button>
      </form>
    </Sheet>
  );
}

/** TaskPage is everything about one Task. */
export function TaskPage({ taskId, header, onOpenThread }: { taskId: string; header: ReactNode; onOpenThread: (id: string) => void }) {
  const { data: t, error } = useTaskDetail(taskId);
  const { members } = useCtx();
  if (!t) return <section className="flex h-full flex-col">{header}<div className="p-4"><ErrorNote>{error}</ErrorNote></div></section>;
  const active = t.runs.find((r) => r.status === "running" || r.status === "pending");
  const diff = t.artifacts.find((a) => a.kind === "diff");
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-4xl space-y-6 px-4 py-5">
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <Pill tone={t.merged_at ? "success" : taskTone(t.status)}>{t.merged_at ? "Merged" : taskLabel[t.status] ?? t.status}</Pill>
              {t.branch && <span className="font-mono text-[12px] text-bb-subtle">{t.branch}</span>}
              {t.pr_url && (
                <a href={t.pr_url} target="_blank" rel="noreferrer noopener" className="text-[12.5px] text-bb-accent hover:underline">
                  Pull request
                </a>
              )}
              {t.thread_id && (
                <Button size="sm" tone="ghost" onClick={() => onOpenThread(t.thread_id!)}>
                  Open thread
                </Button>
              )}
            </div>
            <h1 className="text-[22px] leading-tight font-semibold">{t.title}</h1>
            {t.body && (
              <div className="text-[14px] leading-relaxed text-bb-muted">
                <Markdown text={t.body} />
              </div>
            )}
          </div>

          {active && <RunPanel runId={active.id} />}

          <div>
            <h2 className="mb-2 text-[13px] font-semibold text-bb-muted">Runs</h2>
            {t.runs.length === 0 ? (
              <p className="text-[13px] text-bb-subtle">No Runs yet. Hand the Task to a Bot from its thread.</p>
            ) : (
              <div className="space-y-2">
                {t.runs
                  .filter((r) => r.id !== active?.id)
                  .map((r) => (
                    <RunPanel key={r.id} runId={r.id} defaultOpen={false} />
                  ))}
              </div>
            )}
          </div>

          {diff && <ArtifactView artifact={diff} title="Changes" />}

          {t.artifacts.filter((a) => a.kind === "log").length > 0 && (
            <div>
              <h2 className="mb-2 text-[13px] font-semibold text-bb-muted">Transcripts</h2>
              <div className="space-y-1">
                {t.artifacts
                  .filter((a) => a.kind === "log")
                  .map((a) => (
                    <ArtifactView key={a.id} artifact={a} title={a.name} collapsed />
                  ))}
              </div>
            </div>
          )}

          {t.pipelines.length > 0 && (
            <div>
              <h2 className="mb-2 text-[13px] font-semibold text-bb-muted">CI</h2>
              <ul className="space-y-1 text-[13px]">
                {t.pipelines.map((p) => (
                  <li key={p.id} className="flex items-center gap-2">
                    <Pill tone={p.status === "success" ? "success" : p.status === "failure" ? "danger" : "run"}>{p.status}</Pill>
                    <span>{p.name}</span>
                    {p.commit && <span className="font-mono text-[11.5px] text-bb-subtle">{p.commit.slice(0, 10)}</span>}
                    {p.external_url && (
                      <a href={p.external_url} target="_blank" rel="noreferrer noopener" className="text-bb-accent hover:underline">
                        details
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {t.handoffs.length > 0 && (
            <div>
              <h2 className="mb-2 text-[13px] font-semibold text-bb-muted">Handoffs</h2>
              <ol className="space-y-2 text-[13px]">
                {t.handoffs.map((h) => (
                  <li key={h.id} className="flex gap-2">
                    <Pill tone={h.status === "open" ? runTone("running") : "neutral"}>{h.status}</Pill>
                    <span className="min-w-0">
                      <span className="font-medium">{members.get(h.from_member_id)?.display_name}</span>
                      <span className="text-bb-subtle"> → </span>
                      <span className="font-medium">{members.get(h.to_member_id)?.display_name}</span>
                      {h.note && <span className="block truncate text-bb-subtle">{h.note}</span>}
                    </span>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

function ArtifactView({ artifact, title, collapsed }: { artifact: Artifact; title: string; collapsed?: boolean }) {
  const [body, setBody] = useState<string | undefined>(artifact.body);
  const [open, setOpen] = useState(!collapsed);
  useEffect(() => {
    if (!open || body !== undefined) return;
    let alive = true;
    void api.artifact(artifact.id).then((a) => alive && setBody(a.body ?? ""));
    return () => {
      alive = false;
    };
  }, [open, body, artifact.id]);
  return (
    <div className="rounded-lg border border-bb-border bg-bb-surface">
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full items-center justify-between px-3 py-2 text-left text-[13px]">
        <span className="font-medium">{title}</span>
        <span className="text-bb-subtle">
          {(artifact.size / 1024).toFixed(1)} KB {open ? "▾" : "▸"}
        </span>
      </button>
      {open && (
        <pre className="max-h-[32rem] overflow-auto border-t border-bb-border px-3 py-2 font-mono text-[12px] leading-relaxed">
          {body === undefined ? "Loading…" : artifact.kind === "diff" ? <DiffLines text={body} /> : body}
        </pre>
      )}
    </div>
  );
}

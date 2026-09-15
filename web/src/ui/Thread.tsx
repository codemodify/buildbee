import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { useTopic } from "../live";
import { go } from "../route";
import { useLoad, useThread } from "../store";
import type { TaskDetail } from "../types";
import { Composer } from "./Composer";
import { useCtx } from "./context";
import { Button, ErrorNote, Pill, taskLabel, taskTone } from "./kit";
import { MessageItem, MessageList } from "./Messages";
import { RunPanel } from "./RunPanel";

/** ThreadPanel is a thread beside its Channel; a Task's thread carries its live Run. */
export function ThreadPanel({ rootId, onClose }: { rootId: string; onClose: () => void }) {
  const { thread, error, add } = useThread(rootId);
  const { data } = useCtx();
  const end = useRef<HTMLDivElement>(null);
  const count = thread?.replies.length ?? 0;
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" }); // may return a Promise: not a cleanup
  }, [count]);
  const taskId = thread?.root.task_id;
  return (
    <aside className="flex h-full min-w-0 flex-col border-l border-bb-border bg-bb-surface">
      <header className="flex h-12 shrink-0 items-center justify-between border-b border-bb-border px-4">
        <p className="text-[14px] font-semibold">{taskId ? "Task" : "Thread"}</p>
        <Button tone="ghost" size="sm" onClick={onClose} aria-label="Close thread">
          ✕
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <ErrorNote>{error}</ErrorNote>
        {taskId && <TaskHeader taskId={taskId} />}
        {thread && (
          <>
            <div className="py-2">
              <MessageItem message={thread.root} compactThread hideTask={!!taskId} />
            </div>
            {count > 0 && (
              <div className="flex items-center gap-3 px-4 text-[11.5px] text-bb-subtle">
                {count} {count === 1 ? "reply" : "replies"}
                <span className="h-px flex-1 bg-bb-border" />
              </div>
            )}
            <MessageList messages={thread.replies} compactThread />
          </>
        )}
        <div ref={end} className="h-3" />
      </div>
      <div className="shrink-0 p-3">
        <Composer
          members={data.members}
          placeholder="Reply"
          hint={taskId ? "Reaches the agent while it works · @Bot hands it on" : undefined}
          onSend={async (body) => add(await api.reply(rootId, body))}
        />
      </div>
    </aside>
  );
}

/** useTaskDetail is a Task with its Runs, refreshed as the Project moves. */
export function useTaskDetail(taskId: string) {
  const { data: project } = useCtx();
  const detail = useLoad<TaskDetail>(() => api.taskDetail(taskId), [taskId]);
  const { reload } = detail;
  const timer = useRef<number | undefined>(undefined);
  useTopic(`project:${project.project.id}`, null, () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(reload, 300);
  });
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return detail;
}

function TaskHeader({ taskId }: { taskId: string }) {
  const { data: detail, error, reload } = useTaskDetail(taskId);
  const { data, members } = useCtx();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  if (!detail) return error ? <ErrorNote>{error}</ErrorNote> : null;
  const active = detail.runs.find((r) => r.status === "running" || r.status === "pending");
  const latest = active ?? detail.runs[0];
  const assignee = detail.assignee_member_id ? members.get(detail.assignee_member_id) : undefined;
  async function act(fn: () => Promise<unknown>) {
    setBusy(true);
    setErr("");
    try {
      await fn();
      reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  const handTo = (role: string) => act(() => api.handOff(taskId, { to_role: role, autorun: true, note: "" }));
  return (
    <section className="space-y-3 border-b border-bb-border bg-bb-inset/50 px-4 py-3">
      <div className="flex items-start justify-between gap-3">
        <button type="button" className="min-w-0 text-left" onClick={() => go({ view: "task", projectId: data.project.id, taskId })}>
          <h3 className="text-[15px] leading-snug font-semibold hover:underline">{detail.title}</h3>
          <p className="mt-0.5 text-[12px] text-bb-subtle">
            {assignee?.display_name ?? "Unassigned"}
            {detail.branch && <span className="font-mono"> · {detail.branch}</span>}
          </p>
        </button>
        <Pill tone={detail.merged_at ? "success" : taskTone(detail.status)}>
          {detail.merged_at ? "Merged" : taskLabel[detail.status] ?? detail.status}
        </Pill>
      </div>
      {detail.pr_url && (
        <a href={detail.pr_url} target="_blank" rel="noreferrer noopener" className="block text-[12.5px] text-bb-accent hover:underline">
          {detail.pr_url}
        </a>
      )}
      {latest && <RunPanel key={latest.id} runId={latest.id} defaultOpen={!!active} />}
      <div className="flex flex-wrap gap-1.5">
        {!active && detail.status !== "done" && (
          <>
            <Button size="sm" disabled={busy} onClick={() => void handTo("scout")}>
              Plan
            </Button>
            <Button size="sm" disabled={busy} onClick={() => void handTo("builder")}>
              Build
            </Button>
            {detail.branch && (
              <Button size="sm" disabled={busy} onClick={() => void handTo("sentry")}>
                Review
              </Button>
            )}
          </>
        )}
        {detail.status !== "done" && detail.status !== "canceled" && (
          <Button size="sm" tone="ghost" disabled={busy} onClick={() => void act(() => api.updateTask(taskId, { status: "done" }))}>
            Done
          </Button>
        )}
        {detail.status !== "canceled" && detail.status !== "done" && (
          <Button size="sm" tone="ghost" disabled={busy} onClick={() => void act(() => api.updateTask(taskId, { status: "canceled" }))}>
            Cancel
          </Button>
        )}
      </div>
      <ErrorNote>{err}</ErrorNote>
    </section>
  );
}

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
        <p className="text-[14px] font-semibold">Thread</p>
        <Button tone="ghost" size="sm" onClick={onClose} aria-label="Close thread">
          ✕
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <ErrorNote>{error}</ErrorNote>
        {taskId && <WorkStrip taskId={taskId} />}
        {thread && (
          <>
            <div className="py-2">
              <MessageItem message={thread.root} compactThread />
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
          channelId={thread?.root.channel_id}
          onSend={async (body, fileIds) => add(await api.reply(rootId, body, fileIds))}
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

/**
 * WorkStrip is what a thread shows besides the answers: who is working on
 * it, and a way into each Bot's reasoning, tools and log. The answers are
 * the replies themselves, so nothing here repeats them — a finished Run is
 * one collapsed line. Branch, pull request and the work buttons appear only
 * once a Bot has actually changed the repo.
 */
function WorkStrip({ taskId }: { taskId: string }) {
  const { data: detail, error, reload } = useTaskDetail(taskId);
  const { data } = useCtx();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  if (!detail) return error ? <ErrorNote>{error}</ErrorNote> : null;
  const working = detail.runs.filter((r) => r.status === "running" || r.status === "pending");
  // Every Bot asked shows once: its live Run, or its last one.
  const runs = [...working, ...detail.runs.filter((r) => !working.includes(r)).slice(0, 4)];
  const repo = !!detail.branch || !!detail.pr_url;
  if (!runs.length && !repo) return null;
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
  return (
    <section className="space-y-2 border-b border-bb-border bg-bb-inset/50 px-4 py-3">
      {working.length > 0 && (
        <p className="text-[12px] text-bb-subtle">
          {working.length === 1 ? "1 bot working" : `${working.length} bots working`}
        </p>
      )}
      {runs.map((r) => (
        <RunPanel key={r.id} runId={r.id} defaultOpen={false} />
      ))}
      {repo && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pt-1 text-[12.5px]">
          {detail.branch && <span className="font-mono text-bb-muted">{detail.branch}</span>}
          <Pill tone={detail.merged_at ? "success" : taskTone(detail.status)}>
            {detail.merged_at ? "Merged" : taskLabel[detail.status] ?? detail.status}
          </Pill>
          {detail.pr_url && (
            <a href={detail.pr_url} target="_blank" rel="noreferrer noopener" className="text-bb-accent hover:underline">
              Pull request
            </a>
          )}
          <button
            type="button"
            className="text-bb-muted hover:text-bb-fg"
            onClick={() => go({ view: "task", projectId: data.project.id, taskId })}
          >
            Open
          </button>
          {!working.length && detail.status !== "done" && (
            <Button size="sm" disabled={busy} onClick={() => void act(() => api.handOff(taskId, { to_role: "sentry", autorun: true, note: "" }))}>
              Review
            </Button>
          )}
          {detail.status !== "done" && detail.status !== "canceled" && (
            <Button size="sm" tone="ghost" disabled={busy} onClick={() => void act(() => api.updateTask(taskId, { status: "done" }))}>
              Done
            </Button>
          )}
        </div>
      )}
      <ErrorNote>{err}</ErrorNote>
    </section>
  );
}

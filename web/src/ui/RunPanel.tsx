import { useMemo, useState } from "react";
import { api } from "../api";
import { useRun } from "../store";
import { Markdown, tokens } from "../text";
import type { Run, RunEvent } from "../types";
import { useCtx } from "./context";
import { Avatar, Button, Pill, cx, inputClass, runTone } from "./kit";

const kindVerb: Record<string, string> = { plan: "Planning", build: "Building", review: "Reviewing", merge: "Merging" };
const kindNoun: Record<string, string> = { plan: "Plan", build: "Build", review: "Review", merge: "Merge" };

/** RunPanel follows one Run live: what the agent says, its plan and tools. */
export function RunPanel({ runId, defaultOpen = true }: { runId: string; defaultOpen?: boolean }) {
  const { run, events } = useRun(runId);
  const { members } = useCtx();
  const [open, setOpen] = useState(defaultOpen);
  const view = useMemo(() => digest(events), [events]);
  if (!run) return <div className="rounded-lg border border-bb-border p-3 text-[13px] text-bb-subtle">Loading Run…</div>;
  const bot = run.bot_member_id ? members.get(run.bot_member_id) : undefined;
  const active = run.status === "running" || run.status === "pending";
  return (
    <div className={cx("rounded-lg border bg-bb-surface", active ? "border-bb-run/40" : "border-bb-border")}>
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 px-3 py-2 text-left">
        <Avatar member={bot} name={run.kind === "merge" ? "Merge" : undefined} size={22} />
        <span className="min-w-0 flex-1 truncate text-[13px]">
          <span className="font-semibold">{bot?.display_name ?? "BuildBee"}</span>{" "}
          <span className="text-bb-muted">
            {active ? kindVerb[run.kind ?? "build"] : kindNoun[run.kind ?? "build"]}
            {run.agent ? ` · ${run.agent}` : ""}
            {run.worker ? ` on ${run.worker}` : ""}
          </span>
        </span>
        {run.verdict && <Pill tone={run.verdict === "approve" ? "success" : "danger"}>{run.verdict === "approve" ? "Approved" : "Changes"}</Pill>}
        <Pill tone={runTone(run.status)} pulse={active}>
          {run.status === "pending" ? "Queued" : run.status}
        </Pill>
        <span className="text-bb-subtle">{open ? "▾" : "▸"}</span>
      </button>
      {open && (
        <div className="space-y-3 border-t border-bb-border px-3 py-3">
          {run.status === "pending" && <p className="text-[13px] text-bb-subtle">Waiting for a worker{run.agent ? ` (${run.agent})` : ""}.</p>}
          {view.plan.length > 0 && <PlanView entries={view.plan} />}
          {view.steps.length > 0 && <Steps steps={view.steps} />}
          {view.reply && (
            <div className="text-[13.5px] leading-relaxed">
              <Markdown text={view.reply} />
              {active && <span className="bb-pulse ml-0.5 inline-block h-3 w-1.5 translate-y-0.5 bg-bb-run" />}
            </div>
          )}
          {!view.reply && !active && run.summary && <Markdown text={run.summary} />}
          {(run.status === "failed" || run.status === "canceled") && (
            <p className="rounded-md bg-bb-danger-soft px-2.5 py-1.5 text-[12.5px] text-bb-danger">{run.detail}</p>
          )}
          <Footer run={run} />
          {view.thoughts && <Collapsible label="Reasoning" text={view.thoughts} />}
          {view.logs && <Collapsible label="Log" text={view.logs} mono />}
          {active && run.kind !== "merge" && <Steer run={run} />}
        </div>
      )}
    </div>
  );
}

type Step = { id: string; name: string; kind?: string; status: string; ok?: boolean; summary?: string; paths?: string[] };
type Digest = { reply: string; thoughts: string; logs: string; plan: { content: string; status: string }[]; steps: Step[] };

/** digest folds a Run's events into what a person reads. */
function digest(events: RunEvent[]): Digest {
  const d: Digest = { reply: "", thoughts: "", logs: "", plan: [], steps: [] };
  const byId = new Map<string, Step>();
  for (const ev of events) {
    const p = ev.payload;
    switch (ev.kind) {
      case "token":
        d.reply += String(p.text ?? "");
        break;
      case "thought":
        d.thoughts += String(p.text ?? "");
        break;
      case "log":
        d.logs += String(p.text ?? "");
        break;
      case "steer":
        d.reply += `\n\n> **${String(p.by ?? "Someone")}:** ${String(p.text ?? "")}\n\n`;
        break;
      case "plan":
        d.plan = (p.entries as { content: string; status: string }[]) ?? [];
        break;
      case "tool_call": {
        const s: Step = {
          id: String(p.id ?? ev.seq),
          name: String(p.name ?? "tool"),
          kind: p.kind ? String(p.kind) : undefined,
          status: String(p.status ?? "pending"),
          paths: Array.isArray(p.paths) ? (p.paths as string[]) : undefined,
        };
        byId.set(s.id, s);
        d.steps.push(s);
        break;
      }
      case "tool_result": {
        const s = byId.get(String(p.id ?? ""));
        if (s) {
          if (p.status) s.status = String(p.status);
          if (p.name) s.name = String(p.name);
          if (typeof p.ok === "boolean") s.ok = p.ok;
          if (p.summary) s.summary = String(p.summary);
        }
        break;
      }
    }
  }
  d.reply = d.reply.trim();
  return d;
}

function PlanView({ entries }: { entries: { content: string; status: string }[] }) {
  return (
    <ol className="space-y-1 text-[13px]">
      {entries.map((e, i) => (
        <li key={i} className="flex items-start gap-2">
          <span
            className={cx(
              "mt-1 h-3 w-3 shrink-0 rounded-full border",
              e.status === "completed" && "border-bb-success bg-bb-success",
              e.status === "in_progress" && "border-bb-run bb-pulse bg-bb-run-soft",
              e.status === "pending" && "border-bb-border",
            )}
          />
          <span className={cx(e.status === "completed" && "text-bb-subtle line-through")}>{e.content}</span>
        </li>
      ))}
    </ol>
  );
}

function Steps({ steps }: { steps: Step[] }) {
  const [all, setAll] = useState(false);
  const shown = all ? steps : steps.slice(-6);
  return (
    <div className="space-y-1">
      {steps.length > shown.length && (
        <button type="button" className="text-[12px] text-bb-subtle hover:text-bb-fg" onClick={() => setAll(true)}>
          {steps.length - shown.length} earlier steps
        </button>
      )}
      {shown.map((s) => (
        <div key={s.id} className="flex items-start gap-2 text-[12.5px]">
          <span
            className={cx(
              "mt-1 h-2 w-2 shrink-0 rounded-full",
              s.ok === false || s.status === "failed" ? "bg-bb-danger" : s.status === "completed" ? "bg-bb-success" : "bb-pulse bg-bb-run",
            )}
          />
          <span className="min-w-0">
            <span className="font-medium">{s.name}</span>
            {s.paths && s.paths.length > 0 && <span className="ml-1 font-mono text-bb-subtle">{s.paths.join(", ")}</span>}
            {s.summary && <span className="block truncate text-bb-subtle">{s.summary}</span>}
          </span>
        </div>
      ))}
    </div>
  );
}

function Footer({ run }: { run: Run }) {
  const bits: string[] = [];
  if (run.context_tokens) bits.push(`${tokens(run.context_tokens)} context`);
  if (run.cost) bits.push(run.cost_currency === "USD" || !run.cost_currency ? `$${run.cost.toFixed(2)}` : `${run.cost.toFixed(2)} ${run.cost_currency}`);
  if (run.started_at) {
    const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now();
    const secs = Math.max(0, Math.round((end - new Date(run.started_at).getTime()) / 1000));
    bits.push(secs < 90 ? `${secs}s` : `${Math.round(secs / 60)} min`);
  }
  if (run.branch) bits.push(run.branch);
  if (!bits.length && !run.pr_url) return null;
  return (
    <p className="flex flex-wrap gap-x-3 text-[11.5px] text-bb-subtle">
      {bits.map((b) => (
        <span key={b}>{b}</span>
      ))}
      {run.pr_url && (
        <a className="text-bb-accent hover:underline" href={run.pr_url} target="_blank" rel="noreferrer noopener">
          Pull request
        </a>
      )}
    </p>
  );
}

function Collapsible({ label, text, mono }: { label: string; text: string; mono?: boolean }) {
  return (
    <details className="text-[12.5px]">
      <summary className="cursor-pointer text-bb-subtle hover:text-bb-fg">{label}</summary>
      <pre className={cx("mt-1 max-h-72 overflow-auto rounded-md bg-bb-inset p-2 whitespace-pre-wrap", mono && "font-mono text-[11.5px]")}>{text}</pre>
    </details>
  );
}

/** Steer sends the working agent a message, or stops it. */
function Steer({ run }: { run: Run }) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  async function send(interrupt: boolean) {
    if (!text.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await api.steer(run.id, text.trim(), interrupt);
      setText("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-1.5">
      <div className="flex gap-1.5">
        <input
          className={inputClass}
          value={text}
          placeholder="Message the agent"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void send(false)}
        />
        <Button size="sm" disabled={busy || !text.trim()} onClick={() => void send(false)} title="Sent after its current step">
          Send
        </Button>
        <Button size="sm" disabled={busy || !text.trim()} onClick={() => void send(true)} title="Stops what it is doing and reads this now">
          Interrupt
        </Button>
      </div>
      <div className="flex items-center justify-between">
        <span className="text-[11.5px] text-bb-danger">{err}</span>
        <Button size="sm" tone="danger" disabled={busy} onClick={() => void api.cancelRun(run.id).catch((e: unknown) => setErr(String(e)))}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

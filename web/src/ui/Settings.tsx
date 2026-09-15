import { useEffect, useState, type ReactNode } from "react";
import { api, type ProjectPatch } from "../api";
import { go } from "../route";
import { useLoad } from "../store";
import { ago, Markdown, money, tokens } from "../text";
import type { Member, Routine, Usage } from "../types";
import { useCtx } from "./context";
import { Avatar, Button, ErrorNote, Field, Pill, Toggle, cx, inputBase, inputClass } from "./kit";

export const agents = ["", "claude", "codex", "grok", "opencode", "goose", "fake"];

/** DecisionList is one Project's open Decisions, answerable; nothing when none. */
export function DecisionList() {
  const { data, reload } = useCtx();
  const [err, setErr] = useState("");
  if (data.decisions.length === 0) return null;
  return (
    <section className="space-y-2">
      <h2 className="text-[12px] font-semibold tracking-wide text-bb-subtle uppercase">
        {data.project.name} <span className="font-normal">{data.decisions.length}</span>
      </h2>
      <ErrorNote>{err}</ErrorNote>
      {
        <div className="space-y-3">
          {data.decisions.map((d) => (
            <div key={d.id} className="rounded-lg border border-bb-border bg-bb-surface p-4">
              <div className="text-[14px] leading-relaxed">
                <Markdown text={d.prompt} />
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-2">
                {(d.options.length ? d.options : ["yes", "no"]).map((o) => (
                  <Button
                    key={o}
                    size="sm"
                    tone={o === d.recommendation ? "primary" : "plain"}
                    onClick={async () => {
                      try {
                        await api.answer(d.id, o);
                        reload();
                      } catch (e) {
                        setErr(e instanceof Error ? e.message : String(e));
                      }
                    }}
                  >
                    {o}
                  </Button>
                ))}
                {d.task_id && (
                  <button type="button" className="ml-auto text-[12.5px] text-bb-muted hover:text-bb-fg" onClick={() => go({ view: "task", projectId: data.project.id, taskId: d.task_id! })}>
                    Task
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      }
    </section>
  );
}

/** Settings is how a Project works: autopilot, repo, guidance, Bots, Routines. */
export function Settings({ header }: { header: ReactNode }) {
  const { data, reload } = useCtx();
  const p = data.project;
  const [err, setErr] = useState("");
  async function save(patch: ProjectPatch) {
    setErr("");
    try {
      await api.updateProject(p.id, patch);
      reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Page header={header}>
      <ErrorNote>{err}</ErrorNote>
      <Card title="Autopilot">
        <Row label="Autopilot" hint="Scout → Builder → Sentry → merge, unattended.">
          <Toggle label="Autopilot" checked={!!p.auto_run} onChange={(v) => void save({ auto_run: v })} />
        </Row>
        <Row label="Merging" hint="Auto: after approval and green CI. Approval: ask first.">
          <select className={cx(inputBase, "w-40")} value={p.merge_policy ?? "auto"} onChange={(e) => void save({ merge_policy: e.target.value })}>
            <option value="auto">Auto</option>
            <option value="approval">Approval</option>
          </select>
        </Row>
        <Row label="Runs at once" hint="0 = no limit.">
          <NumberInput value={p.max_runs ?? 0} onSave={(v) => void save({ max_runs: v })} />
        </Row>
      </Card>

      <Card title="Repository">
        <TextSetting label="Repo URL" value={p.repo_url ?? ""} placeholder="git@github.com:acme/app.git" onSave={(v) => save({ repo_url: v })} />
        <TextSetting label="Default branch" value={p.default_branch ?? ""} placeholder="main" onSave={(v) => save({ default_branch: v })} />
      </Card>

      <Card title="Instructions" subtitle="Read by every agent.">
        <TextSetting multiline value={p.instructions ?? ""} onSave={(v) => save({ instructions: v })} />
      </Card>

      <Card title="Bots">
        <div className="divide-y divide-bb-border">
          {data.members
            .filter((m) => m.kind === "bot")
            .map((m) => (
              <BotRow key={m.id} bot={m} onSaved={reload} />
            ))}
        </div>
      </Card>

      <Routines />

      <Card title="Membership">
        <Row label="Leave Project" hint="Your messages stay. Writing here again brings you back.">
          <Button
            size="sm"
            tone="danger"
            onClick={async () => {
              try {
                await api.leave(p.id);
                reload();
              } catch (e) {
                setErr(e instanceof Error ? e.message : String(e));
              }
            }}
          >
            Leave
          </Button>
        </Row>
      </Card>
    </Page>
  );
}

function BotRow({ bot, onSaved }: { bot: Member; onSaved: () => void }) {
  const [instr, setInstr] = useState(bot.instructions ?? "");
  const [err, setErr] = useState("");
  useEffect(() => {
    setInstr(bot.instructions ?? "");
  }, [bot.instructions]);
  async function save(patch: { agent?: string; instructions?: string }) {
    try {
      await api.updateMember(bot.id, patch);
      onSaved();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <div className="space-y-2 py-3">
      <div className="flex items-center gap-2.5">
        <Avatar member={bot} size={26} />
        <span className="font-medium">{bot.display_name}</span>
        <Pill tone="bot">{bot.role}</Pill>
        <select className={cx(inputBase, "ml-auto w-36")} value={bot.agent ?? ""} onChange={(e) => void save({ agent: e.target.value })} aria-label={`${bot.display_name}'s agent`}>
          {agents.map((a) => (
            <option key={a} value={a}>
              {a || "Any agent"}
            </option>
          ))}
        </select>
      </div>
      <textarea
        className={cx(inputClass, "min-h-16 text-[13px]")}
        value={instr}
        onChange={(e) => setInstr(e.target.value)}
        onBlur={() => instr !== (bot.instructions ?? "") && void save({ instructions: instr })}
      />
      <ErrorNote>{err}</ErrorNote>
    </div>
  );
}

function Routines() {
  const { data } = useCtx();
  const { data: list, reload } = useLoad(() => api.routines(data.project.id), [data.project.id]);
  const [adding, setAdding] = useState(false);
  const [err, setErr] = useState("");
  const bots = data.members.filter((m) => m.kind === "bot" && ["scout", "builder", "sentry"].includes(m.role));
  async function act(fn: () => Promise<unknown>) {
    setErr("");
    try {
      await fn();
      reload();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Card title="Routines" subtitle="Scheduled prompts; each run opens a Task." action={<Button size="sm" onClick={() => setAdding(true)}>New</Button>}>
      <ErrorNote>{err}</ErrorNote>
      {adding && <NewRoutine bots={bots} onDone={() => (setAdding(false), reload())} onCancel={() => setAdding(false)} />}

      <div className="divide-y divide-bb-border">
        {(list?.items ?? []).map((r: Routine) => (
          <div key={r.id} className="flex items-start gap-3 py-3">
            <Toggle label={`Enable ${r.name}`} checked={r.enabled} onChange={(v) => void act(() => api.updateRoutine(r.id, { enabled: v }))} />
            <div className="min-w-0 flex-1">
              <p className="text-[13.5px] font-medium">
                {r.name} <span className="font-normal text-bb-subtle">every {r.schedule}</span>
              </p>
              <p className="truncate text-[12.5px] text-bb-subtle">{r.prompt}</p>
              {r.last_run_at && <p className="text-[11.5px] text-bb-subtle">Last {ago(r.last_run_at)} ago</p>}
            </div>
            <Button size="sm" onClick={() => void act(() => api.fireRoutine(r.id))}>
              Run
            </Button>
          </div>
        ))}
      </div>
    </Card>
  );
}

function NewRoutine({ bots, onDone, onCancel }: { bots: Member[]; onDone: () => void; onCancel: () => void }) {
  const { data } = useCtx();
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [schedule, setSchedule] = useState("168h");
  const [bot, setBot] = useState("");
  const [err, setErr] = useState("");
  return (
    <form
      className="mb-3 space-y-2 rounded-md border border-bb-border p-3"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await api.createRoutine(data.project.id, { name, prompt, schedule, enabled: true, bot_member_id: bot || undefined });
          onDone();
        } catch (e2) {
          setErr(e2 instanceof Error ? e2.message : String(e2));
        }
      }}
    >
      <input className={inputClass} placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      <textarea className={cx(inputClass, "min-h-20")} placeholder="Prompt" value={prompt} onChange={(e) => setPrompt(e.target.value)} />
      <div className="flex flex-wrap gap-2">
        <select className={cx(inputBase, "w-auto")} value={schedule} onChange={(e) => setSchedule(e.target.value)} aria-label="Schedule">
          <option value="1h">Every hour</option>
          <option value="24h">Every day</option>
          <option value="168h">Every week</option>
        </select>
        <select className={cx(inputBase, "w-auto")} value={bot} onChange={(e) => setBot(e.target.value)} aria-label="Bot">
          <option value="">Scout</option>
          {bots
            .filter((b) => b.role !== "scout")
            .map((b) => (
              <option key={b.id} value={b.id}>
                {b.display_name}
              </option>
            ))}
        </select>
        <Button tone="primary" type="submit" disabled={!name.trim() || !prompt.trim()}>
          Save
        </Button>
        <Button tone="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
      <ErrorNote>{err}</ErrorNote>
    </form>
  );
}

/** UsagePanel sums what agents used, by agent and by Project. */
export function UsagePanel() {
  const [days, setDays] = useState(30);
  const { data: u, error } = useLoad<Usage>(() => api.usage(undefined, days), [days]);
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        {[1, 7, 30, 90].map((d) => (
          <Button key={d} size="sm" tone={d === days ? "primary" : "plain"} onClick={() => setDays(d)}>
            {d === 1 ? "Today" : `${d} days`}
          </Button>
        ))}
      </div>
      <ErrorNote>{error}</ErrorNote>
      {u && (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <Stat label="Runs" value={String(u.runs)} />
            <Stat label="Cost reported" value={money(u.cost)} />
            <Stat label="Agents used" value={String(u.by_agent.length)} />
          </div>
          <UsageTable title="By agent" rows={u.by_agent} />
          {u.by_project && <UsageTable title="By Project" rows={u.by_project} />}
          <p className="text-[12px] text-bb-subtle">As reported by agents.</p>
        </>
      )}
    </div>
  );
}

function UsageTable({ title, rows }: { title: string; rows: Usage["by_agent"] }) {
  return (
    <Card title={title}>
      <div className="overflow-x-auto">
        <table className="w-full text-[13px] tabular-nums">
          <thead className="text-left text-[12px] text-bb-subtle">
            <tr>
              <th className="py-1.5 font-medium">Name</th>
              <th className="py-1.5 text-right font-medium">Runs</th>
              <th className="py-1.5 text-right font-medium">Context used</th>
              <th className="py-1.5 text-right font-medium">Cost</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-bb-border">
            {rows.map((r) => (
              <tr key={r.key}>
                <td className="py-1.5">{r.name || r.key}</td>
                <td className="py-1.5 text-right">{r.runs}</td>
                <td className="py-1.5 text-right">{tokens(r.context_tokens)}</td>
                <td className="py-1.5 text-right">{money(r.cost)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-bb-border bg-bb-surface px-4 py-3">
      <p className="text-[12px] text-bb-subtle">{label}</p>
      <p className="mt-0.5 text-[20px] font-semibold tabular-nums">{value}</p>
    </div>
  );
}

function Page({ header, children }: { header: ReactNode; children: ReactNode }) {
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-4 px-4 py-5">{children}</div>
      </div>
    </section>
  );
}

function Card({ title, subtitle, action, children }: { title: string; subtitle?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="rounded-lg border border-bb-border bg-bb-surface p-4">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div>
          <h2 className="text-[14px] font-semibold">{title}</h2>
          {subtitle && <p className="text-[12.5px] text-bb-subtle">{subtitle}</p>}
        </div>
        {action}
      </div>
      <div className="space-y-3">{children}</div>
    </section>
  );
}

function Row({ label, hint, children }: { label: string; hint: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="min-w-0">
        <p className="text-[13.5px] font-medium">{label}</p>
        <p className="text-[12px] text-bb-subtle">{hint}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function TextSetting({
  label,
  value,
  placeholder,
  multiline,
  onSave,
}: {
  label?: string;
  value: string;
  placeholder?: string;
  multiline?: boolean;
  onSave: (v: string) => Promise<void>;
}) {
  const [v, setV] = useState(value);
  useEffect(() => {
    setV(value);
  }, [value]);
  const dirty = v !== value;
  const input = multiline ? (
    <textarea className={cx(inputClass, "min-h-28 font-mono text-[12.5px]")} value={v} placeholder={placeholder} onChange={(e) => setV(e.target.value)} />
  ) : (
    <input className={inputClass} value={v} placeholder={placeholder} onChange={(e) => setV(e.target.value)} />
  );
  return (
    <div className="space-y-1.5">
      {label ? <Field label={label}>{input}</Field> : input}
      {dirty && (
        <div className="flex gap-2">
          <Button size="sm" tone="primary" onClick={() => void onSave(v.trim())}>
            Save
          </Button>
          <Button size="sm" tone="ghost" onClick={() => setV(value)}>
            Discard
          </Button>
        </div>
      )}
    </div>
  );
}

function NumberInput({ value, onSave }: { value: number; onSave: (v: number) => void }) {
  const [v, setV] = useState(String(value));
  useEffect(() => {
    setV(String(value));
  }, [value]);
  return (
    <input
      type="number"
      min={0}
      max={1000}
      className={cx(inputBase, "w-24 text-right")}
      value={v}
      onChange={(e) => setV(e.target.value)}
      onBlur={() => Number(v) !== value && onSave(Math.max(0, Math.floor(Number(v) || 0)))}
      aria-label="Runs at once"
    />
  );
}

import { useState, type FormEvent, type ReactNode } from "react";
import { api } from "../api";
import { useLiveStatus, useTopic } from "../live";
import { go } from "../route";
import { useLoad } from "../store";
import { ago } from "../text";
import type { AgentLoad, Person, Presence, Project, Roster } from "../types";
import { Avatar, Button, ErrorNote, Field, Pill, Sheet, cx, inputBase, inputClass } from "./kit";
import { ProjectScope } from "./scope";
import { UsagePanel, agents } from "./Settings";
import { TaskBoard } from "./Tasks";

/** invitedName is the name in an invite link (#/hi/<name>), if this is one. */
export function invitedName(): string {
  const m = /^#\/hi\/([^?]+)/.exec(window.location.hash);
  if (!m) return "";
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return "";
  }
}

/** Status is the Server at a glance: agents, Tasks, people, Bots, usage. */
export function Status({ me, projects, presence, header }: { me: Person; projects: Project[]; presence?: Presence; header: ReactNode }) {
  const { data: r, error, reload } = useLoad<Roster>(() => api.roster(), []);
  const live = useLiveStatus();
  const [dialog, setDialog] = useState<"invite" | "bot" | null>(null);
  useTopic("presence:server", null, reload);
  const online = new Set((presence?.people ?? []).map((p) => p.id));
  const byProject = new Map<string, Roster["bots"]>();
  for (const b of r?.bots ?? []) byProject.set(b.project_id, [...(byProject.get(b.project_id) ?? []), b]);
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-6 px-4 py-5">
          <ErrorNote>{error}</ErrorNote>
          <Agents presence={presence} bots={r?.bots ?? []} />

          <section className="space-y-3">
            <GroupTitle title="Tasks" />
            {projects.map((p) => (
              <ProjectScope key={p.id} projectId={p.id} me={me} presence={presence}>
                <TaskBoard />
              </ProjectScope>
            ))}
          </section>

          <Group
            title="People"
            count={r?.people.length}
            action={
              <Button size="sm" onClick={() => setDialog("invite")} disabled={!projects.length}>
                Invite
              </Button>
            }
          >
            {(r?.people ?? []).map((p) => (
              <Row key={p.id}>
                <Avatar name={p.name} size={28} online={online.has(p.id)} />
                <span className="w-32 shrink-0 truncate font-medium">{p.name}</span>
                <span className="flex min-w-0 flex-wrap gap-1">
                  {p.projects.map((m) => (
                    <button key={m.member_id} type="button" onClick={() => go({ view: "channel", projectId: m.project_id })} title={m.left_at ? `Left ${ago(m.left_at)} ago` : m.role}>
                      <Pill tone={m.left_at ? "neutral" : "accent"}>
                        <span className={cx(m.left_at && "line-through")}>{m.project_name}</span>
                      </Pill>
                    </button>
                  ))}
                </span>
                <span className="ml-auto flex shrink-0 items-center gap-2 text-[12px] text-bb-subtle">
                  {p.id === me.id ? (
                    <>
                      <span className={cx(live !== "open" && "text-amber-600")}>{live === "open" ? "you" : "you · reconnecting"}</span>
                      <button
                        type="button"
                        className="rounded px-1.5 py-0.5 hover:bg-bb-hover hover:text-bb-fg"
                        onClick={async () => {
                          await api.forgetMe();
                          window.location.reload();
                        }}
                      >
                        Switch
                      </button>
                    </>
                  ) : (
                    online.has(p.id) && "online"
                  )}
                </span>
              </Row>
            ))}
          </Group>

          <Group
            title="Bots"
            count={r?.bots.length}
            action={
              <Button size="sm" onClick={() => setDialog("bot")} disabled={!projects.length}>
                Add bot
              </Button>
            }
          >
            {[...byProject.entries()].map(([projectId, bots]) => (
              <div key={projectId} className="py-1.5">
                <p className="px-3 pb-1 text-[12px] text-bb-subtle">{bots[0].project_name}</p>
                {bots.map((b) => (
                  <button key={b.id} type="button" className="block w-full text-left hover:bg-bb-hover" onClick={() => go({ view: "settings", projectId })} title="Edit in the Project's Settings">
                    <Row>
                      <Avatar member={b} size={24} />
                      <span className="w-32 shrink-0 truncate font-medium">{b.display_name}</span>
                      <Pill tone="bot">{b.role}</Pill>
                      <span className="ml-auto text-[12px] text-bb-subtle">{b.agent || "any agent"}</span>
                    </Row>
                  </button>
                ))}
              </div>
            ))}
          </Group>

          <section>
            <GroupTitle title="Usage" />
            <UsagePanel />
          </section>

          <Group title="Recent">
            {(r?.events ?? []).length === 0 && <p className="px-3 py-2 text-[13px] text-bb-subtle">—</p>}
            {(r?.events ?? []).map((e, i) => (
              <Row key={i}>
                <span className="text-[13px]">
                  <span className="font-medium">{e.name}</span> {verb[e.action] ?? e.action} <span className="font-medium">{e.project_name}</span>
                </span>
                <span className="ml-auto text-[12px] text-bb-subtle">{ago(e.at)}</span>
              </Row>
            ))}
          </Group>
        </div>
      </div>
      {dialog === "invite" && <Invite projects={projects} onClose={() => setDialog(null)} onDone={reload} />}
      {dialog === "bot" && <AddBot projects={projects} onClose={() => setDialog(null)} onDone={reload} />}
    </section>
  );
}

/** Agents is which agents can work now, how busy they are, and what waits in vain. */
function Agents({ presence, bots }: { presence?: Presence; bots: Roster["bots"] }) {
  if (!presence) return null;
  const { workers, local } = presence;
  const offered = new Set(workers.flatMap((w) => w.agents));
  const realOnline = [...offered].some((a) => a !== "fake");
  const warnings: string[] = [];
  for (const a of presence.agents) {
    if (a.agent === "any" && a.queued > 0 && !realOnline) warnings.push(`${a.queued} waiting for any agent. None is online.`);
    else if (a.agent !== "any" && a.workers === 0 && a.queued > 0) warnings.push(`${a.queued} waiting for ${a.agent}. No machine offers it.`);
  }
  const idleBots = new Map<string, string[]>();
  for (const b of bots) if (b.agent && !offered.has(b.agent)) idleBots.set(b.agent, [...(idleBots.get(b.agent) ?? []), `${b.display_name} (${b.project_name})`]);
  if (workers.length > 0) for (const [agent, names] of idleBots) warnings.push(`${names.join(", ")} ${names.length > 1 ? "use" : "uses"} ${agent}. No machine offers it.`);
  const rows = presence.agents.filter((a) => a.agent !== "any" || a.running > 0 || a.queued > 0);
  const note =
    local.state === "running"
      ? local.isolation === "host"
        ? "This server runs agents directly on its machine (no Docker)."
        : "This server runs agents in containers."
      : local.state === "starting"
        ? "Checking which agents this server can run…"
        : local.state === "unavailable"
          ? `This server isn't running agents. ${local.reason ?? ""}`
          : workers.length === 0
            ? local.reason
            : undefined;
  return (
    <Group title="Agents" count={rows.filter((a) => a.workers > 0).length} action={presence.slots > 0 && <span className="text-[12px] text-bb-subtle tabular-nums">{presence.running} of {presence.slots} busy</span>}>
      {warnings.map((w) => (
        <p key={w} className="bg-bb-danger-soft px-3 py-2 text-[13px] text-bb-danger">
          {w}
        </p>
      ))}
      {rows.length === 0 && <p className="px-3 py-2 text-[13px] text-bb-subtle">None online. Tasks wait until one is.</p>}
      {rows.map((a) => (
        <Row key={a.agent}>
          <span className={cx("h-2 w-2 shrink-0 rounded-full", a.workers > 0 || a.agent === "any" ? "bg-bb-success" : "bg-bb-danger")} />
          <span className="w-32 shrink-0 truncate font-medium">{a.agent === "any" ? "any agent" : a.agent}</span>
          <span className="text-[12px] text-bb-subtle">{a.agent === "any" ? "" : a.workers === 0 ? "offline" : `${a.workers} machine${a.workers > 1 ? "s" : ""}`}</span>
          <span className="ml-auto text-[12px] text-bb-subtle tabular-nums">{load(a)}</span>
        </Row>
      ))}
      {note && <p className="px-3 py-2 text-[12.5px] text-bb-subtle">{note}</p>}
      {workers.length > 0 && (
        <details className="px-3 py-2 text-[13px]">
          <summary className="cursor-pointer text-[12.5px] text-bb-subtle hover:text-bb-fg">Machines {workers.length}</summary>
          <div className="mt-1 divide-y divide-bb-border">
            {workers.map((w) => (
              <div key={w.name} className="flex items-center gap-3 py-1.5">
                <span className="w-40 shrink-0 truncate">
                  {w.name}
                  {w.local && <span className="ml-1.5 text-[11.5px] text-bb-subtle">this server</span>}
                </span>
                <span className="flex min-w-0 flex-wrap gap-1">
                  {w.agents.map((ag) => (
                    <Pill key={ag}>{ag}</Pill>
                  ))}
                </span>
                <span className="ml-auto shrink-0 text-[12px] text-bb-subtle tabular-nums">
                  {w.running}
                  {w.slots ? `/${w.slots}` : ""} busy · {ago(w.last_seen)}
                </span>
              </div>
            ))}
          </div>
        </details>
      )}
    </Group>
  );
}

function load(a: AgentLoad): string {
  const bits = [a.running && `${a.running} running`, a.queued && `${a.queued} queued`].filter(Boolean);
  return bits.length ? bits.join(" · ") : "idle";
}

const verb: Record<string, string> = { added: "was added to", left: "left", joined: "joined", created: "created" };

/** Invite adds a person to Projects and gives a link that signs them in by name. */
function Invite({ projects, onClose, onDone }: { projects: Project[]; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<Set<string>>(() => new Set(projects.map((p) => p.id)));
  const [link, setLink] = useState("");
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      for (const id of picked) await api.addMember(id, { kind: "human", display_name: name.trim() });
      setLink(`${window.location.origin}${window.location.pathname}#/hi/${encodeURIComponent(name.trim())}`);
      onDone();
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : String(e2));
    }
  }
  const local = /^(localhost|127\.|\[::1\])/.test(window.location.hostname);
  if (link)
    return (
      <Sheet title={`Invite ${name.trim()}`} onClose={onClose}>
        <div className="space-y-3">
          <Field label="Link" hint={local ? "This is a localhost link. Others need the Server's LAN address." : "Opens with the name filled in."}>
            <input id="invite-link" className={inputClass} readOnly value={link} onFocus={(e) => e.currentTarget.select()} />
          </Field>
          <div className="flex gap-2">
            <Button tone="primary" onClick={() => void copy(link).then(setCopied)}>
              {copied ? "Copied" : "Copy"}
            </Button>
            <Button onClick={onClose}>Done</Button>
          </div>
        </div>
      </Sheet>
    );
  return (
    <Sheet title="Invite" onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <Field label="Name">
          <input id="invite-name" className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Bob" />
        </Field>
        <fieldset className="space-y-1">
          <legend className="mb-1 text-[12.5px] font-medium text-bb-muted">Projects</legend>
          {projects.map((p) => (
            <label key={p.id} className="flex items-center gap-2 text-[13.5px]">
              <input
                type="checkbox"
                checked={picked.has(p.id)}
                onChange={(e) =>
                  setPicked((s) => {
                    const next = new Set(s);
                    if (e.target.checked) next.add(p.id);
                    else next.delete(p.id);
                    return next;
                  })
                }
              />
              {p.name}
            </label>
          ))}
        </fieldset>
        <ErrorNote>{err}</ErrorNote>
        <Button tone="primary" type="submit" disabled={!name.trim() || picked.size === 0}>
          Invite
        </Button>
      </form>
    </Sheet>
  );
}

/** AddBot adds a Bot to one Project. */
function AddBot({ projects, onClose, onDone }: { projects: Project[]; onClose: () => void; onDone: () => void }) {
  const [projectId, setProjectId] = useState(projects[0]?.id ?? "");
  const [name, setName] = useState("");
  const [role, setRole] = useState("");
  const [agent, setAgent] = useState("");
  const [instructions, setInstructions] = useState("");
  const [err, setErr] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      await api.addMember(projectId, { kind: "bot", display_name: name.trim(), role: role.trim(), agent, instructions: instructions.trim() });
      onDone();
      onClose();
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : String(e2));
    }
  }
  return (
    <Sheet title="Add bot" onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <Field label="Project">
          <select id="bot-project" className={cx(inputBase, "w-full")} value={projectId} onChange={(e) => setProjectId(e.target.value)}>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name">
            <input id="bot-name" className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Docs" />
          </Field>
          <Field label="Role">
            <input id="bot-role" className={inputClass} value={role} onChange={(e) => setRole(e.target.value)} placeholder="writer" />
          </Field>
        </div>
        <Field label="Agent">
          <select id="bot-agent" className={cx(inputBase, "w-full")} value={agent} onChange={(e) => setAgent(e.target.value)}>
            {agents.map((a) => (
              <option key={a} value={a}>
                {a || "Any agent"}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Instructions">
          <textarea
            id="bot-instructions"
            className={cx(inputClass, "min-h-20 text-[13px]")}
            value={instructions}
            onChange={(e) => setInstructions(e.target.value)}
            placeholder="You write and fix docs."
          />
        </Field>
        <ErrorNote>{err}</ErrorNote>
        <Button tone="primary" type="submit" disabled={!projectId || !name.trim() || !role.trim()}>
          Add
        </Button>
      </form>
    </Sheet>
  );
}

/** copy puts text on the clipboard, also over plain http on the LAN. */
async function copy(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const el = document.getElementById("invite-link") as HTMLInputElement | null;
    el?.select();
    return document.execCommand("copy");
  }
}

function GroupTitle({ title, count, action }: { title: string; count?: number; action?: ReactNode }) {
  return (
    <div className="mb-1.5 flex items-center justify-between gap-2">
      <h2 className="text-[12px] font-semibold tracking-wide text-bb-subtle uppercase">
        {title} {count !== undefined && <span className="font-normal">{count}</span>}
      </h2>
      {action}
    </div>
  );
}

function Group({ title, count, action, children }: { title: string; count?: number; action?: ReactNode; children: ReactNode }) {
  return (
    <section>
      <GroupTitle title={title} count={count} action={action} />
      <div className="divide-y divide-bb-border rounded-lg border border-bb-border bg-bb-surface">{children}</div>
    </section>
  );
}

function Row({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-3 px-3 py-2 text-[13.5px]">{children}</div>;
}

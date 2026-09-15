import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { useLiveStatus } from "../live";
import { go, type Route } from "../route";
import { useMemberIndex, useProject, useUnread, type ProjectData } from "../store";
import type { Channel, Member, Person, Presence, Project } from "../types";
import { Avatar, Button, Field, Sheet, cx, inputClass } from "./kit";

/** dmName names a DM by the other people in it. */
export function dmName(c: Channel, members: Map<string, Member>, myMemberId?: string): string {
  const others = (c.member_ids ?? []).filter((id) => id !== myMemberId).map((id) => members.get(id)?.display_name ?? "someone");
  return others.join(", ") || c.name;
}

const OPEN_KEY = "buildbee.sidebar.closed";

function loadClosed(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem(OPEN_KEY) || "{}") as Record<string, boolean>;
  } catch {
    return {};
  }
}

/** Sidebar lists every Project as a section that opens and closes. */
export function Sidebar({
  me,
  route,
  projects,
  presence,
  onNavigate,
  onProjectsChanged,
}: {
  me: Person;
  route: Route;
  projects: Project[];
  presence?: Presence;
  onNavigate: () => void;
  onProjectsChanged: () => void;
}) {
  const [closed, setClosed] = useState<Record<string, boolean>>(loadClosed);
  const [creating, setCreating] = useState(false);
  useEffect(() => {
    try {
      localStorage.setItem(OPEN_KEY, JSON.stringify(closed));
    } catch {
      /* private mode */
    }
  }, [closed]);
  const allClosed = projects.length > 0 && projects.every((p) => closed[p.id]);
  const setAll = (value: boolean) => setClosed(Object.fromEntries(projects.map((p) => [p.id, value])));
  const online = new Set((presence?.people ?? []).map((p) => p.id));
  return (
    <nav className="flex h-full w-full flex-col bg-bb-sidebar">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-bb-border px-3">
        <span className="flex h-6 w-6 items-center justify-center rounded-md bg-bb-accent-strong text-[12px] font-bold text-stone-950">B</span>
        <span className="text-[14px] font-semibold">BuildBee</span>
        <span className="ml-auto flex items-center gap-0.5">
          <IconButton label={allClosed ? "Open all" : "Close all"} onClick={() => setAll(!allClosed)}>
            {allClosed ? "⊞" : "⊟"}
          </IconButton>
          <IconButton label="New Project" onClick={() => setCreating(true)}>
            +
          </IconButton>
        </span>
      </div>
      <div className="min-h-0 flex-1 space-y-1 overflow-y-auto px-2 py-2">
        {projects.map((p) => (
          <ProjectSection
            key={p.id}
            project={p}
            me={me}
            route={route}
            online={online}
            open={!closed[p.id]}
            onToggle={() => setClosed((c) => ({ ...c, [p.id]: !c[p.id] }))}
            onNavigate={onNavigate}
          />
        ))}
      </div>
      <Footer me={me.name} presence={presence} onNavigate={onNavigate} />
      {creating && (
        <NewProject
          onClose={() => {
            setCreating(false);
            onProjectsChanged();
          }}
        />
      )}
    </nav>
  );
}

function ProjectSection({
  project,
  me,
  route,
  online,
  open,
  onToggle,
  onNavigate,
}: {
  project: Project;
  me: Person;
  route: Route;
  online: Set<string>;
  open: boolean;
  onToggle: () => void;
  onNavigate: () => void;
}) {
  const { data, reload } = useProject(project.id);
  const members = useMemberIndex(data?.members);
  const myMember = data?.members.find((m) => m.person_id === me.id && !m.left_at);
  const here = "projectId" in route && route.projectId === project.id;
  const activeChannel = here && route.view === "channel" ? route.channelId ?? data?.channels[0]?.id : undefined;
  const channels = [...(data?.channels ?? []), ...(data?.dms ?? [])];
  const unread = useUnread(project.id, channels.map((c) => c.id), activeChannel, myMember?.id);
  const total = Object.values(unread).reduce((n, u) => n + u.unread, 0);
  const [dialog, setDialog] = useState<"channel" | "dm" | null>(null);
  const openRoute = (r: Route) => {
    go(r);
    onNavigate();
  };
  const pid = project.id;
  const inProgress = data?.tasks.filter((t) => t.status === "in_progress").length ?? 0;
  return (
    <section>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className={cx("flex h-8 w-full items-center gap-1.5 rounded-md px-1.5 text-left hover:bg-bb-hover", here && "text-bb-fg")}
      >
        <span className={cx("w-3 text-[10px] text-bb-subtle transition-transform", open && "rotate-90")}>▶</span>
        <span className={cx("min-w-0 flex-1 truncate text-[13.5px]", here ? "font-semibold" : "font-medium text-bb-muted")}>{project.name}</span>
        {project.auto_run && <span className="h-1.5 w-1.5 rounded-full bg-bb-accent-strong" title="Autopilot" />}
        {!open && total > 0 && <Count n={total} />}
      </button>
      {open && data && (
        <div className="mb-2 ml-3 space-y-px border-l border-bb-border pl-2">
          {data.channels.map((c) => (
            <Item key={c.id} active={activeChannel === c.id} count={unread[c.id]?.unread} onClick={() => openRoute({ view: "channel", projectId: pid, channelId: c.id })}>
              <span className="w-3.5 text-center text-bb-subtle">#</span>
              <span className="truncate">{c.name}</span>
              {c.locked && <span className="text-[10px] text-bb-subtle" title="Pinned">📌</span>}
            </Item>
          ))}
          {data.dms.map((c) => {
            const other = (c.member_ids ?? []).map((id) => members.get(id)).find((m) => m && m.id !== myMember?.id);
            return (
              <Item key={c.id} active={activeChannel === c.id} count={unread[c.id]?.unread} onClick={() => openRoute({ view: "channel", projectId: pid, channelId: c.id })}>
                <Avatar member={other} size={16} online={other?.person_id ? online.has(other.person_id) : undefined} />
                <span className="truncate">{dmName(c, members, myMember?.id)}</span>
              </Item>
            );
          })}
          <div className="flex gap-1 py-0.5 pl-1">
            <SmallLink onClick={() => setDialog("channel")}>+ channel</SmallLink>
            <SmallLink onClick={() => setDialog("dm")}>+ message</SmallLink>
          </div>
          <Item active={here && (route.view === "tasks" || route.view === "task")} count={inProgress || undefined} quiet onClick={() => openRoute({ view: "tasks", projectId: pid })}>
            <Glyph d="M3 4h10M3 8h10M3 12h6" />
            Tasks
          </Item>
          <Item active={here && route.view === "decisions"} count={data.decisions.length || undefined} onClick={() => openRoute({ view: "decisions", projectId: pid })}>
            <Glyph d="M8 2v5l3 2M8 14A6 6 0 108 2a6 6 0 000 12z" />
            Decisions
          </Item>
          <Item active={here && route.view === "settings"} quiet onClick={() => openRoute({ view: "settings", projectId: pid })}>
            <Glyph d="M8 10a2 2 0 100-4 2 2 0 000 4zM8 1v2M8 13v2M1 8h2M13 8h2" />
            Settings
          </Item>
        </div>
      )}
      {dialog === "channel" && <NewChannel projectId={pid} onClose={() => setDialog(null)} onDone={reload} />}
      {dialog === "dm" && data && <NewDM data={data} myMemberId={myMember?.id} online={online} onClose={() => setDialog(null)} onDone={reload} />}
    </section>
  );
}

function Count({ n }: { n: number }) {
  return <span className="rounded-full bg-bb-accent-strong px-1.5 text-[11px] font-semibold text-stone-950">{n}</span>;
}

function Item({ active, count, quiet, onClick, children }: { active?: boolean; count?: number; quiet?: boolean; onClick: () => void; children: ReactNode }) {
  const unread = !!count && !quiet;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cx(
        "flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-left text-[13.5px]",
        active ? "bg-bb-accent-soft font-medium text-bb-fg" : "text-bb-muted hover:bg-bb-hover hover:text-bb-fg",
        unread && !active && "font-semibold text-bb-fg",
      )}
    >
      {children}
      {!!count && (quiet ? <span className="ml-auto text-[11px] text-bb-subtle">{count}</span> : <span className="ml-auto"><Count n={count} /></span>)}
    </button>
  );
}

function SmallLink({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="rounded px-1 text-[12px] text-bb-subtle hover:bg-bb-hover hover:text-bb-fg">
      {children}
    </button>
  );
}

function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" onClick={onClick} aria-label={label} title={label} className="flex h-7 w-7 items-center justify-center rounded-md text-[15px] text-bb-muted hover:bg-bb-hover hover:text-bb-fg">
      {children}
    </button>
  );
}

function Glyph({ d }: { d: string }) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" className="w-3.5 shrink-0">
      <path d={d} />
    </svg>
  );
}

function Footer({ me, presence, onNavigate }: { me: string; presence?: Presence; onNavigate: () => void }) {
  const status = useLiveStatus();
  const workers = presence?.workers ?? [];
  const agents = [...new Set(workers.flatMap((w) => w.agents))];
  return (
    <div className="shrink-0 space-y-1 border-t border-bb-border px-3 py-2.5 text-[12px] text-bb-subtle">
      <p className="flex items-center gap-1.5" title={workers.map((w) => `${w.name}: ${w.agents.join(", ")}`).join("\n")}>
        <span className={cx("h-2 w-2 rounded-full", workers.length ? "bg-emerald-500" : "bg-bb-border")} />
        {workers.length ? `${workers.length} worker${workers.length > 1 ? "s" : ""} · ${agents.join(", ")}` : "No workers"}
        <button
          type="button"
          className="ml-auto hover:text-bb-fg"
          onClick={() => {
            go({ view: "usage" });
            onNavigate();
          }}
        >
          Usage
        </button>
      </p>
      <p className="flex items-center justify-between">
        <span className="truncate">
          <span className={cx("mr-1.5 inline-block h-2 w-2 rounded-full", status === "open" ? "bg-emerald-500" : "bb-pulse bg-amber-500")} />
          {me}
        </span>
        <button
          type="button"
          className="hover:text-bb-fg"
          onClick={async () => {
            await api.forgetMe();
            window.location.reload();
          }}
        >
          Switch
        </button>
      </p>
    </div>
  );
}

function NewChannel({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState("");
  const [err, setErr] = useState("");
  return (
    <Sheet title="New channel" onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            const c = await api.createChannel(projectId, name.trim().replace(/^#/, ""));
            onDone();
            go({ view: "channel", projectId, channelId: c.id });
            onClose();
          } catch (e2) {
            setErr(e2 instanceof Error ? e2.message : String(e2));
          }
        }}
      >
        <Field label="Name" hint={err}>
          <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="releases" />
        </Field>
        <Button tone="primary" type="submit" disabled={!name.trim()}>
          Create
        </Button>
      </form>
    </Sheet>
  );
}

function NewDM({
  data,
  myMemberId,
  online,
  onClose,
  onDone,
}: {
  data: ProjectData;
  myMemberId?: string;
  online: Set<string>;
  onClose: () => void;
  onDone: () => void;
}) {
  const [err, setErr] = useState("");
  const others = data.members.filter((m) => m.id !== myMemberId && !m.left_at);
  async function open(m: Member) {
    try {
      const c = await api.openDM(data.project.id, [m.id]);
      onDone();
      go({ view: "channel", projectId: data.project.id, channelId: c.id });
      onClose();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Sheet title="Message" onClose={onClose}>
      <ul className="space-y-px">
        {others.map((m) => (
          <li key={m.id}>
            <button type="button" onClick={() => void open(m)} className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-bb-hover">
              <Avatar member={m} size={24} online={m.person_id ? online.has(m.person_id) : undefined} />
              <span className="text-[13.5px] font-medium">{m.display_name}</span>
              <span className="text-[12px] text-bb-subtle">{m.kind === "bot" ? `${m.role}${m.agent ? ` · ${m.agent}` : ""}` : m.role}</span>
            </button>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-[12px] text-bb-danger">{err}</p>
    </Sheet>
  );
}

export function NewProject({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState("");
  const [err, setErr] = useState("");
  return (
    <Sheet title="New Project" onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            const p = await api.createProject(name.trim());
            onClose();
            go({ view: "channel", projectId: p.id });
          } catch (e2) {
            setErr(e2 instanceof Error ? e2.message : String(e2));
          }
        }}
      >
        <Field label="Name" hint={err}>
          <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Button tone="primary" type="submit" disabled={!name.trim()}>
          Create
        </Button>
      </form>
    </Sheet>
  );
}

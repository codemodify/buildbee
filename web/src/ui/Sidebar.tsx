import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { useLiveStatus } from "../live";
import { go, type Route } from "../route";
import { useDMs, useLoad, useProject, useUnread } from "../store";
import type { Channel, DM, Member, Person, Presence, Project, Roster } from "../types";
import { Avatar, Button, Sheet, cx, inputClass } from "./kit";

/** dmName names a DM by the other people in it. */
export function dmName(c: Channel, members: Map<string, Member>, myMemberId?: string): string {
  const others = (c.member_ids ?? []).filter((id) => id !== myMemberId).map((id) => members.get(id)?.display_name ?? "someone");
  return others.join(", ") || c.name;
}

const OPEN_KEY = "buildbee.sidebar.closed";
const DM_KEY = "dm"; // the DM group's entry among the closed sections; closed unless opened

function loadClosed(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem(OPEN_KEY) || "{}") as Record<string, boolean>;
  } catch {
    return {};
  }
}

/** Sidebar is the Server's pinned places, DMs, then every Project as a section that opens and closes. */
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
  const [decisions, setDecisions] = useState<Record<string, number>>({});
  useEffect(() => {
    try {
      localStorage.setItem(OPEN_KEY, JSON.stringify(closed));
    } catch {
      /* private mode */
    }
  }, [closed]);
  const keys = [DM_KEY, ...projects.map((p) => p.id)];
  const allClosed = keys.every((k) => closed[k] ?? k === DM_KEY);
  const setAll = (value: boolean) => setClosed(Object.fromEntries(keys.map((k) => [k, value])));
  const isClosed = (k: string) => closed[k] ?? k === DM_KEY; // DMs start closed
  const toggle = (k: string) => setClosed((c) => ({ ...c, [k]: !isClosed(k) }));
  const online = new Set((presence?.people ?? []).map((p) => p.id));
  const live = useLiveStatus();
  const open = (r: Route) => {
    go(r);
    onNavigate();
  };
  const openDecisions = Object.values(decisions).reduce((n, d) => n + d, 0);
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
        <div className="space-y-px">
          <Pinned name="status" active={route.view === "status"} onClick={() => open({ view: "status" })}>
            {live !== "open" ? (
              <span className="bb-pulse ml-auto h-1.5 w-1.5 rounded-full bg-amber-500" title="Reconnecting" />
            ) : (
              online.size > 0 && (
                <span className="ml-auto flex items-center gap-1 text-[11px] text-bb-subtle" title={`${online.size} online`}>
                  <span className="h-1.5 w-1.5 rounded-full bg-bb-success" />
                  {online.size}
                </span>
              )
            )}
          </Pinned>
          <Pinned name="decisions" active={route.view === "decisions"} onClick={() => open({ view: "decisions" })}>
            {openDecisions > 0 && (
              <span className="ml-auto">
                <Count n={openDecisions} />
              </span>
            )}
          </Pinned>
        </div>
        <DMSection me={me} route={route} online={online} open={!isClosed(DM_KEY)} onToggle={() => toggle(DM_KEY)} onOpen={open} />
        {projects.map((p) => (
          <ProjectSection
            key={p.id}
            project={p}
            me={me}
            route={route}
            open={!closed[p.id]}
            onToggle={() => toggle(p.id)}
            onOpen={open}
            onDecisions={(n) => setDecisions((d) => (d[p.id] === n ? d : { ...d, [p.id]: n }))}
          />
        ))}
      </div>
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

/** Pinned is one of the Server's fixed places at the top. */
function Pinned({ name, active, onClick, children }: { name: string; active: boolean; onClick: () => void; children?: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cx(
        "flex h-8 w-full items-center gap-2 rounded-md px-1.5 text-left text-[13.5px]",
        active ? "bg-bb-accent-soft font-medium text-bb-fg" : "text-bb-muted hover:bg-bb-hover hover:text-bb-fg",
      )}
    >
      <span className="w-3 text-center text-bb-subtle">#</span>
      {name}
      <span className="text-[10px] text-bb-subtle" title="Pinned">
        📌
      </span>
      {children}
    </button>
  );
}

/** SectionRow is a section's header: a toggle, then its actions on the right. */
function SectionRow({
  label,
  open,
  here,
  onToggle,
  badge,
  children,
}: {
  label: ReactNode;
  open: boolean;
  here?: boolean;
  onToggle: () => void;
  badge?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="group flex h-8 items-center rounded-md pr-0.5 hover:bg-bb-hover">
      <button type="button" onClick={onToggle} aria-expanded={open} className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-1.5 text-left">
        <span className={cx("w-3 text-[10px] text-bb-subtle transition-transform", open && "rotate-90")}>▶</span>
        <span className={cx("min-w-0 truncate text-[13.5px]", here ? "font-semibold text-bb-fg" : "font-medium text-bb-muted")}>{label}</span>
        {badge}
      </button>
      {children}
    </div>
  );
}

/** DMSection is every DM the Person is in with other people. */
function DMSection({
  me,
  route,
  online,
  open,
  onToggle,
  onOpen,
}: {
  me: Person;
  route: Route;
  online: Set<string>;
  open: boolean;
  onToggle: () => void;
  onOpen: (r: Route) => void;
}) {
  const activeId = route.view === "channel" ? route.channelId : undefined;
  const { dms, reload } = useDMs(me.id, activeId);
  const [adding, setAdding] = useState(false);
  const total = dms.reduce((n, d) => n + (d.id === activeId ? 0 : d.unread), 0);
  return (
    <section>
      <SectionRow label="DM" open={open} onToggle={onToggle} badge={!open && total > 0 && <Count n={total} />}>
        <RowAction label="New message" onClick={() => setAdding(true)}>
          +
        </RowAction>
      </SectionRow>
      {open && (
        <div className="mb-2 ml-3 space-y-px border-l border-bb-border pl-2">
          {dms.length === 0 && (
            <button type="button" className="px-1.5 py-1 text-[12.5px] text-bb-subtle hover:text-bb-fg" onClick={() => setAdding(true)}>
              Message someone
            </button>
          )}
          {dms.map((d) => (
            <DMItem
              key={d.id}
              dm={d}
              active={d.id === activeId}
              online={online}
              onClick={() => onOpen({ view: "channel", projectId: d.project_id, channelId: d.id })}
              onClose={async () => {
                await api.closeDM(d.id).catch(() => undefined);
                reload();
                if (d.id === activeId) onOpen({ view: "status" });
              }}
            />
          ))}
        </div>
      )}
      {adding && (
        <NewMessage
          me={me}
          online={online}
          onClose={() => setAdding(false)}
          onOpened={(c) => {
            reload();
            setAdding(false);
            if (!open) onToggle();
            onOpen({ view: "channel", projectId: c.project_id, channelId: c.id });
          }}
        />
      )}
    </section>
  );
}

function DMItem({ dm, active, online, onClick, onClose }: { dm: DM; active: boolean; online: Set<string>; onClick: () => void; onClose: () => void }) {
  const first = dm.with[0];
  const name = dm.with.map((w) => w.name).join(", ") || dm.name;
  return (
    <div className="group/dm relative">
      <Item active={active} count={active ? 0 : dm.unread} onClick={onClick}>
        <Avatar name={name} size={16} online={first?.person_id ? online.has(first.person_id) : undefined} />
        <span className="truncate pr-5">{name}</span>
      </Item>
      <button
        type="button"
        onClick={onClose}
        aria-label={`Close the DM with ${name}`}
        title="Close (it comes back with a new message)"
        className="absolute top-1/2 right-1 hidden h-5 w-5 -translate-y-1/2 items-center justify-center rounded text-[12px] text-bb-subtle group-focus-within/dm:flex group-hover/dm:flex hover:bg-bb-border/60 hover:text-bb-fg"
      >
        ✕
      </button>
    </div>
  );
}

function ProjectSection({
  project,
  me,
  route,
  open,
  onToggle,
  onOpen,
  onDecisions,
}: {
  project: Project;
  me: Person;
  route: Route;
  open: boolean;
  onToggle: () => void;
  onOpen: (r: Route) => void;
  onDecisions: (n: number) => void;
}) {
  const { data, reload } = useProject(project.id);
  const myMember = data?.members.find((m) => m.person_id === me.id && !m.left_at);
  const here = "projectId" in route && route.projectId === project.id;
  const activeChannel = here && route.view === "channel" ? (route.channelId ?? data?.channels[0]?.id) : undefined;
  const channels = data?.channels ?? [];
  const unread = useUnread(
    project.id,
    channels.map((c) => c.id),
    activeChannel,
    myMember?.id,
  );
  const total = channels.reduce((n, c) => n + (unread[c.id]?.unread ?? 0), 0);
  const [addingChannel, setAddingChannel] = useState(false);
  const pid = project.id;
  const nDecisions = data?.decisions.length ?? 0;
  useEffect(() => onDecisions(nDecisions), [nDecisions]);
  return (
    <section>
      <SectionRow
        label={project.name}
        open={open}
        here={here}
        onToggle={onToggle}
        badge={
          <>
            {project.auto_run && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-bb-accent-strong" title="Autopilot" />}
            {!open && total > 0 && <Count n={total} />}
          </>
        }
      >
        <RowAction label="New channel" onClick={() => setAddingChannel(true)}>
          +
        </RowAction>
        <RowAction label="Settings" active={here && route.view === "settings"} onClick={() => onOpen({ view: "settings", projectId: pid })}>
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round">
            <path d="M8 10a2 2 0 100-4 2 2 0 000 4zM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4" />
          </svg>
        </RowAction>
      </SectionRow>
      {open && data && (
        <div className="mb-2 ml-3 space-y-px border-l border-bb-border pl-2">
          {channels.map((c) => (
            <Item key={c.id} active={activeChannel === c.id} count={unread[c.id]?.unread} onClick={() => onOpen({ view: "channel", projectId: pid, channelId: c.id })}>
              <span className="w-3.5 text-center text-bb-subtle">#</span>
              <span className="truncate">{c.name}</span>
              {c.locked && (
                <span className="text-[10px] text-bb-subtle" title="Pinned">
                  📌
                </span>
              )}
            </Item>
          ))}
        </div>
      )}
      {addingChannel && <NewChannel projectId={pid} onClose={() => setAddingChannel(false)} onDone={reload} />}
    </section>
  );
}

function Count({ n }: { n: number }) {
  return <span className="rounded-full bg-bb-accent-strong px-1.5 text-[11px] font-semibold text-stone-950">{n}</span>;
}

function Item({ active, count, onClick, children }: { active?: boolean; count?: number; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cx(
        "flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-left text-[13.5px]",
        active ? "bg-bb-accent-soft font-medium text-bb-fg" : "text-bb-muted hover:bg-bb-hover hover:text-bb-fg",
        !!count && !active && "font-semibold text-bb-fg",
      )}
    >
      {children}
      {!!count && (
        <span className="ml-auto">
          <Count n={count} />
        </span>
      )}
    </button>
  );
}

/** RowAction is a small button on a section's row. */
function RowAction({ label, active, onClick, children }: { label: string; active?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={cx(
        "flex h-6 w-6 shrink-0 items-center justify-center rounded text-[15px] hover:bg-bb-border/60 hover:text-bb-fg",
        active ? "text-bb-fg" : "text-bb-subtle",
      )}
    >
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
        <input id="channel-name" className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="channel name" aria-label="Channel name" />
        {err && <p className="text-[12px] text-bb-danger">{err}</p>}
        <div className="flex justify-end">
          <Button tone="primary" type="submit" disabled={!name.trim()}>
            Create
          </Button>
        </div>
      </form>
    </Sheet>
  );
}

/** NewMessage starts a DM with people anywhere on the Server. */
function NewMessage({ me, online, onClose, onOpened }: { me: Person; online: Set<string>; onClose: () => void; onOpened: (c: Channel) => void }) {
  const { data: roster } = useLoad<Roster>(() => api.roster(), []);
  const [filter, setFilter] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [err, setErr] = useState("");
  const f = filter.trim().toLowerCase();
  const people = (roster?.people ?? []).filter((p) => p.id !== me.id && p.name.toLowerCase().includes(f));
  async function open() {
    setErr("");
    try {
      onOpened(await api.openDirect(picked));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Sheet title="New message" onClose={onClose}>
      <div className="space-y-3">
        <input id="dm-filter" className={inputClass} autoFocus value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="name" aria-label="Find people" />
        <ul className="max-h-80 space-y-px overflow-y-auto">
          {people.map((p) => {
            const on = picked.includes(p.id);
            return (
              <li key={p.id}>
                <button
                  type="button"
                  aria-pressed={on}
                  onClick={() => setPicked((ids) => (on ? ids.filter((x) => x !== p.id) : [...ids, p.id]))}
                  className={cx("flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-bb-hover", on && "bg-bb-accent-soft")}
                >
                  <Avatar name={p.name} size={24} online={online.has(p.id)} />
                  <span className="text-[13.5px] font-medium">{p.name}</span>
                  <span className="ml-auto text-[13px] text-bb-accent">{on ? "✓" : ""}</span>
                </button>
              </li>
            );
          })}
          {roster && people.length === 0 && <li className="px-2 text-[13px] text-bb-subtle">{f ? "No one matches." : "No one else here yet. Invite people from # status."}</li>}
        </ul>
        {err && <p className="text-[12px] text-bb-danger">{err}</p>}
        <div className="flex justify-end">
          <Button tone="primary" disabled={picked.length === 0} onClick={() => void open()}>
            Message
          </Button>
        </div>
      </div>
    </Sheet>
  );
}

/** NewProject creates a Project: you, and its #tasks. */
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
        <input id="project-name" className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="project name" aria-label="Project name" />
        {err && <p className="text-[12px] text-bb-danger">{err}</p>}
        <div className="flex justify-end">
          <Button tone="primary" type="submit" disabled={!name.trim()}>
            Create
          </Button>
        </div>
      </form>
    </Sheet>
  );
}

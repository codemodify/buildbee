import { useState, type ReactNode } from "react";
import { api } from "../api";
import { useLiveStatus } from "../live";
import { go, href, type Route } from "../route";
import type { Channel, Member, Presence, Project, Unread } from "../types";
import { useCtx } from "./context";
import { Avatar, Button, Field, Sheet, cx, inputClass } from "./kit";

/** dmName names a DM by the other people in it. */
export function dmName(c: Channel, members: Map<string, Member>, myMemberId?: string): string {
  const others = (c.member_ids ?? []).filter((id) => id !== myMemberId).map((id) => members.get(id)?.display_name ?? "someone");
  return others.join(", ") || c.name;
}

export function Sidebar({
  route,
  projects,
  unread,
  presence,
  onNavigate,
}: {
  route: Route;
  projects: Project[];
  unread: Record<string, Unread>;
  presence?: Presence;
  onNavigate: () => void;
}) {
  const { data, members, online, myMember, me } = useCtx();
  const [dialog, setDialog] = useState<"channel" | "dm" | "project" | null>(null);
  const pid = data.project.id;
  const activeChannel = route.view === "channel" ? route.channelId ?? data.channels[0]?.id : undefined;
  const open = (r: Route) => {
    go(r);
    onNavigate();
  };
  const inProgress = data.tasks.filter((t) => t.status === "in_progress").length;
  return (
    <nav className="flex h-full w-full flex-col bg-bb-sidebar">
      <ProjectSwitcher projects={projects} current={data.project} onNew={() => setDialog("project")} />
      <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-2 py-3">
        <Section title="Channels" action={<Add label="New channel" onClick={() => setDialog("channel")} />}>
          {data.channels.map((c) => (
            <Item
              key={c.id}
              active={activeChannel === c.id}
              count={unread[c.id]?.unread}
              onClick={() => open({ view: "channel", projectId: pid, channelId: c.id })}
            >
              <span className="w-4 text-center text-bb-subtle">#</span>
              <span className="truncate">{c.name}</span>
            </Item>
          ))}
        </Section>
        <Section title="Direct messages" action={<Add label="New message" onClick={() => setDialog("dm")} />}>
          {data.dms.map((c) => {
            const other = (c.member_ids ?? []).map((id) => members.get(id)).find((m) => m && m.id !== myMember?.id);
            return (
              <Item
                key={c.id}
                active={activeChannel === c.id}
                count={unread[c.id]?.unread}
                onClick={() => open({ view: "channel", projectId: pid, channelId: c.id })}
              >
                <Avatar member={other} size={18} online={other?.person_id ? online.has(other.person_id) : undefined} />
                <span className="truncate">{dmName(c, members, myMember?.id)}</span>
              </Item>
            );
          })}
          {data.dms.length === 0 && <p className="px-2 text-[12px] text-bb-subtle">Talk to a person or a Bot directly.</p>}
        </Section>
        <Section title="Work">
          <Item active={route.view === "tasks" || route.view === "task"} count={inProgress || undefined} quiet onClick={() => open({ view: "tasks", projectId: pid })}>
            <Glyph d="M3 4h10M3 8h10M3 12h6" />
            Tasks
          </Item>
          <Item active={route.view === "decisions"} count={data.decisions.length || undefined} onClick={() => open({ view: "decisions", projectId: pid })}>
            <Glyph d="M8 2v5l3 2M8 14A6 6 0 108 2a6 6 0 000 12z" />
            Decisions
          </Item>
          <Item active={route.view === "usage"} quiet onClick={() => open({ view: "usage", projectId: pid })}>
            <Glyph d="M3 13V8M8 13V3M13 13v-7" />
            Usage
          </Item>
          <Item active={route.view === "settings"} quiet onClick={() => open({ view: "settings", projectId: pid })}>
            <Glyph d="M8 10a2 2 0 100-4 2 2 0 000 4zM8 1v2M8 13v2M1 8h2M13 8h2M3 3l1.5 1.5M11.5 11.5L13 13M3 13l1.5-1.5M11.5 4.5L13 3" />
            Settings
          </Item>
        </Section>
      </div>
      <Footer me={me.name} presence={presence} autopilot={!!data.project.auto_run} />
      {dialog === "channel" && <NewChannel projectId={pid} onClose={() => setDialog(null)} />}
      {dialog === "dm" && <NewDM onClose={() => setDialog(null)} />}
      {dialog === "project" && <NewProject onClose={() => setDialog(null)} />}
    </nav>
  );
}

function ProjectSwitcher({ projects, current, onNew }: { projects: Project[]; current: Project; onNew: () => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative shrink-0 border-b border-bb-border px-3 py-2.5">
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left hover:bg-bb-hover">
        <span className="flex h-7 w-7 items-center justify-center rounded-md bg-bb-accent-strong text-[13px] font-bold text-stone-950">
          {current.name.slice(0, 1).toUpperCase()}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[14px] font-semibold">{current.name}</span>
          <span className="block text-[11.5px] text-bb-subtle">{current.auto_run ? "Autopilot on" : "Autopilot off"}</span>
        </span>
        <span className="text-bb-subtle">▾</span>
      </button>
      {open && (
        <div className="absolute inset-x-3 top-full z-30 mt-1 rounded-md border border-bb-border bg-bb-surface py-1 shadow-lg">
          {projects.map((p) => (
            <a
              key={p.id}
              href={href({ view: "channel", projectId: p.id })}
              onClick={() => setOpen(false)}
              className={cx("block truncate px-3 py-1.5 text-[13px] hover:bg-bb-hover", p.id === current.id && "font-semibold")}
            >
              {p.name}
            </a>
          ))}
          <button
            type="button"
            onClick={() => {
              setOpen(false);
              onNew();
            }}
            className="block w-full border-t border-bb-border px-3 py-1.5 text-left text-[13px] text-bb-accent hover:bg-bb-hover"
          >
            New Project…
          </button>
        </div>
      )}
    </div>
  );
}

function Section({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div>
      <div className="mb-0.5 flex items-center justify-between px-2">
        <p className="text-[11.5px] font-semibold tracking-wide text-bb-subtle uppercase">{title}</p>
        {action}
      </div>
      <div className="space-y-px">{children}</div>
    </div>
  );
}

function Item({ active, count, quiet, onClick, children }: { active?: boolean; count?: number; quiet?: boolean; onClick: () => void; children: ReactNode }) {
  const unread = !!count && !quiet;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cx(
        "flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-[13.5px]",
        active ? "bg-bb-accent-soft font-medium text-bb-fg" : "text-bb-muted hover:bg-bb-hover hover:text-bb-fg",
        unread && !active && "font-semibold text-bb-fg",
      )}
    >
      {children}
      {!!count && (
        <span className={cx("ml-auto rounded-full px-1.5 text-[11px] font-semibold", quiet ? "text-bb-subtle" : "bg-bb-accent-strong text-stone-950")}>
          {count}
        </span>
      )}
    </button>
  );
}

function Add({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} aria-label={label} title={label} className="rounded px-1 text-[15px] leading-none text-bb-subtle hover:bg-bb-hover hover:text-bb-fg">
      +
    </button>
  );
}

function Glyph({ d }: { d: string }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" className="shrink-0">
      <path d={d} />
    </svg>
  );
}

function Footer({ me, presence, autopilot }: { me: string; presence?: Presence; autopilot: boolean }) {
  const status = useLiveStatus();
  const workers = presence?.workers ?? [];
  const agents = [...new Set(workers.flatMap((w) => w.agents))].filter((a) => a !== "fake");
  return (
    <div className="shrink-0 space-y-1 border-t border-bb-border px-3 py-2.5 text-[12px] text-bb-subtle">
      <p className="flex items-center gap-1.5" title={workers.map((w) => `${w.name}: ${w.agents.join(", ")}`).join("\n")}>
        <span className={cx("h-2 w-2 rounded-full", workers.length ? "bg-emerald-500" : "bg-bb-border")} />
        {workers.length ? `${workers.length} worker${workers.length > 1 ? "s" : ""}${agents.length ? ` · ${agents.join(", ")}` : ""}` : autopilot ? "No workers online: Runs wait" : "No workers online"}
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
          Switch person
        </button>
      </p>
    </div>
  );
}

function NewChannel({ projectId, onClose }: { projectId: string; onClose: () => void }) {
  const { reload } = useCtx();
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
            reload();
            go({ view: "channel", projectId, channelId: c.id });
            onClose();
          } catch (e2) {
            setErr(e2 instanceof Error ? e2.message : String(e2));
          }
        }}
      >
        <Field label="Name" hint={err || "Lower case, like design or releases."}>
          <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="releases" />
        </Field>
        <Button tone="primary" type="submit" disabled={!name.trim()}>
          Create channel
        </Button>
      </form>
    </Sheet>
  );
}

function NewDM({ onClose }: { onClose: () => void }) {
  const { data, myMember, online, reload } = useCtx();
  const [err, setErr] = useState("");
  const others = data.members.filter((m) => m.id !== myMember?.id);
  async function open(m: Member) {
    try {
      const c = await api.openDM(data.project.id, [m.id]);
      reload();
      go({ view: "channel", projectId: data.project.id, channelId: c.id });
      onClose();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <Sheet title="New message" onClose={onClose}>
      <p className="mb-3 text-[13px] text-bb-subtle">Message a person, or give a Bot work directly.</p>
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
            go({ view: "settings", projectId: p.id });
            window.location.reload();
          } catch (e2) {
            setErr(e2 instanceof Error ? e2.message : String(e2));
          }
        }}
      >
        <Field label="Name" hint={err || "Scout, Builder, Sentry and Pulse join it; connect a repo in its settings."}>
          <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Payments service" />
        </Field>
        <Button tone="primary" type="submit" disabled={!name.trim()}>
          Create Project
        </Button>
      </form>
    </Sheet>
  );
}

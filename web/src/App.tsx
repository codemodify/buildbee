import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { api } from "./api";
import { MeContext } from "./me";
import { go, useRoute, type Route } from "./route";
import { useLoad, useMemberIndex, usePresence, useProject } from "./store";
import type { Message, Person, Presence, Project } from "./types";
import { ChannelHeader, ChannelView, MenuButton } from "./ui/Channel";
import { InboxBell } from "./ui/Inbox";
import { Members } from "./ui/Members";
import { ProjectCtx, type Ctx } from "./ui/context";
import { Button, Empty, ErrorNote, cx, inputClass } from "./ui/kit";
import { Decisions, Settings, UsageView } from "./ui/Settings";
import { Sidebar, dmName } from "./ui/Sidebar";
import { Board, TaskPage } from "./ui/Tasks";
import { ThreadPanel } from "./ui/Thread";

export default function App() {
  const [me, setMe] = useState<Person | null | undefined>(undefined);
  const { data: projects, reload } = useLoad<Project[]>(() => api.projects().then((r) => r.items), [me?.id]);
  useEffect(() => {
    api.me().then((r) => setMe(r.person), () => setMe(null));
  }, []);
  if (me === undefined || !projects) return <main className="h-dvh" />;
  if (me === null) return <Start needProject={projects.length === 0} onDone={setMe} />;
  return (
    <MeContext.Provider value={me}>
      <Shell me={me} projects={projects} reloadProjects={reload} />
    </MeContext.Provider>
  );
}

/** Start asks who you are, and for a first Project when there is none. */
function Start({ needProject, onDone }: { needProject: boolean; onDone: (p: Person) => void }) {
  const [name, setName] = useState("");
  const [project, setProject] = useState("");
  const [error, setError] = useState("");
  const ready = name.trim() && (!needProject || project.trim());
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      const { person } = await api.setMe(name.trim());
      if (needProject) {
        const p = await api.createProject(project.trim());
        go({ view: "channel", projectId: p.id });
      }
      onDone(person);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }
  return (
    <main className="flex h-dvh items-center justify-center px-4">
      <form onSubmit={submit} className="w-full max-w-xs space-y-3">
        <div className="mb-5 flex items-center gap-2">
          <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-bb-accent-strong text-[15px] font-bold text-stone-950">B</span>
          <h1 className="text-[18px] font-semibold">BuildBee</h1>
        </div>
        <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Your name" aria-label="Your name" />
        {needProject && <input className={inputClass} value={project} onChange={(e) => setProject(e.target.value)} placeholder="Project name" aria-label="Project name" />}
        <ErrorNote>{error}</ErrorNote>
        <Button tone="primary" type="submit" disabled={!ready} className="w-full">
          Start
        </Button>
      </form>
    </main>
  );
}

/** Shell is the sidebar with every Project, and the current view. */
function Shell({ me, projects, reloadProjects }: { me: Person; projects: Project[]; reloadProjects: () => void }) {
  const route = useRoute();
  const presence = usePresence();
  const [drawer, setDrawer] = useState(false);
  // On wide screens the sidebar can be hidden to leave the channel and thread.
  const [hidden, setHidden] = useState(() => stored("buildbee.sidebar.hidden") === "1");
  useEffect(() => store("buildbee.sidebar.hidden", hidden ? "1" : "0"), [hidden]);
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "\\") {
        e.preventDefault();
        setHidden((h) => !h);
      }
    };
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, []);
  const projectId = "projectId" in route ? route.projectId : undefined;
  useEffect(() => {
    if (route.view !== "home" || !projects.length) return;
    const last = localStorage.getItem("buildbee.project");
    go({ view: "channel", projectId: (projects.find((p) => p.id === last) ?? projects[0]).id });
  }, [route.view, projects]);
  useEffect(() => {
    if (projectId) localStorage.setItem("buildbee.project", projectId);
    if (projectId && !projects.some((p) => p.id === projectId)) reloadProjects();
  }, [projectId, projects, reloadProjects]);
  const menu = () => (window.matchMedia("(min-width: 1024px)").matches ? setHidden((h) => !h) : setDrawer(true));
  return (
    <div className="flex h-dvh overflow-hidden">
      <div className={cx("fixed inset-0 z-40 bg-black/40 lg:hidden", drawer ? "block" : "hidden")} onClick={() => setDrawer(false)} />
      <aside
        className={cx(
          "fixed inset-y-0 left-0 z-50 w-72 shrink-0 border-r border-bb-border transition-transform lg:static lg:z-auto lg:w-64 lg:translate-x-0",
          drawer ? "translate-x-0" : "-translate-x-full",
          hidden && "lg:hidden",
        )}
      >
        <Sidebar me={me} route={route} projects={projects} presence={presence} onNavigate={() => setDrawer(false)} onProjectsChanged={reloadProjects} />
      </aside>
      <main className="flex min-w-0 flex-1">
        {projectId ? (
          <ProjectView key={projectId} me={me} route={route} projectId={projectId} presence={presence} onMenu={menu} />
        ) : route.view === "members" ? (
          <div className="min-w-0 flex-1">
            <Members header={<TopBar title="# members" onMenu={menu} />} />
          </div>
        ) : route.view === "usage" ? (
          <div className="min-w-0 flex-1">
            <UsageView header={<TopBar title="Usage" onMenu={menu} />} />
          </div>
        ) : (
          <div className="flex min-w-0 flex-1 flex-col">
            <TopBar title="" onMenu={menu} />
            <Empty title="No Projects">Create one with + in the sidebar.</Empty>
          </div>
        )}
      </main>
    </div>
  );
}

/** ProjectView is one Project's current view, with a thread beside it. */
function ProjectView({ me, route, projectId, presence, onMenu }: { me: Person; route: Route; projectId: string; presence?: Presence; onMenu: () => void }) {
  const { data, error, reload } = useProject(projectId);
  const members = useMemberIndex(data?.members);
  const tasks = useMemo(() => new Map((data?.tasks ?? []).map((t) => [t.id, t])), [data?.tasks]);
  const online = useMemo(() => new Set((presence?.people ?? []).map((p) => p.id)), [presence]);
  const threadId = "threadId" in route ? route.threadId : undefined;
  const [threadWidth, setThreadWidth] = useThreadWidth();
  if (!data) return <div className="flex-1 p-4">{error && <ErrorNote>{error}</ErrorNote>}</div>;
  const myMember = data.members.find((m) => m.person_id === me.id && !m.left_at);
  const ctx: Ctx = { me, data, members, tasks, online, myMember, reload };
  const channelId = route.view === "channel" ? route.channelId ?? data.channels[0]?.id : undefined;
  const all = [...data.channels, ...data.dms];
  const found = all.find((c) => c.id === channelId);
  const channel = found && found.kind === "dm" ? { ...found, name: dmName(found, members, myMember?.id) } : found;
  const openThread = (m: Message | string) => {
    const id = typeof m === "string" ? m : m.id;
    if (route.view === "channel") go({ ...route, channelId, threadId: id });
    else go({ view: "channel", projectId, channelId: typeof m === "string" ? undefined : m.channel_id, threadId: id });
  };
  const closeThread = () => route.view === "channel" && go({ ...route, threadId: undefined });
  const titled = (title: string) => <TopBar title={title} sub={data.project.name} onMenu={onMenu} />;

  let main: ReactNode;
  switch (route.view) {
    case "tasks":
      main = <Board header={titled("Tasks")} />;
      break;
    case "task":
      main = <TaskPage taskId={route.taskId} header={titled("Task")} onOpenThread={openThread} />;
      break;
    case "decisions":
      main = <Decisions header={titled("Decisions")} />;
      break;
    case "settings":
      main = <Settings header={titled("Settings")} />;
      break;
    case "usage":
      main = <UsageView header={titled("Usage")} projectId={projectId} />;
      break;
    default:
      main = channel ? (
        <ChannelView key={channel.id} channel={channel} onOpenThread={openThread} header={<ChannelHeader channel={channel} onMenu={onMenu} />} />
      ) : (
        <Empty title="No channel" />
      );
  }
  return (
    <ProjectCtx.Provider value={ctx}>
      <div className={cx("min-w-0 flex-1", threadId && "hidden md:block")}>{main}</div>
      {threadId && (
        <div className="fixed inset-0 z-30 md:relative md:inset-auto md:z-auto md:w-[var(--thread-w)] md:shrink-0" style={{ ["--thread-w" as string]: `${threadWidth}px` }}>
          <ResizeHandle width={threadWidth} onWidth={setThreadWidth} />
          <ThreadPanel key={threadId} rootId={threadId} onClose={closeThread} />
        </div>
      )}
    </ProjectCtx.Provider>
  );
}

function TopBar({ title, sub, onMenu }: { title: string; sub?: string; onMenu: () => void }) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-bb-border px-4">
      <MenuButton onClick={onMenu} />
      <h1 className="text-[15px] font-semibold">{title}</h1>
      {sub && <span className="truncate text-[13px] text-bb-subtle">{sub}</span>}
      <div className="ml-auto">
        <InboxBell />
      </div>
    </header>
  );
}

function stored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function store(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* private mode */
  }
}

const MIN_THREAD = 320;

/** useThreadWidth is the thread panel's width, remembered. */
function useThreadWidth(): [number, (w: number) => void] {
  const [w, setW] = useState(() => Number(stored("buildbee.thread.width")) || 448);
  const set = (next: number) => {
    const clamped = Math.round(Math.min(Math.max(next, MIN_THREAD), window.innerWidth * 0.7));
    setW(clamped);
    store("buildbee.thread.width", String(clamped));
  };
  return [w, set];
}

/** ResizeHandle drags the thread panel's left edge. */
function ResizeHandle({ width, onWidth }: { width: number; onWidth: (w: number) => void }) {
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize thread"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "ArrowLeft") onWidth(width + 24);
        if (e.key === "ArrowRight") onWidth(width - 24);
      }}
      onPointerDown={(e) => {
        const startX = e.clientX;
        const startW = width;
        const el = e.currentTarget;
        el.setPointerCapture(e.pointerId);
        const move = (ev: PointerEvent) => onWidth(startW + (startX - ev.clientX));
        const up = () => {
          el.removeEventListener("pointermove", move);
          el.removeEventListener("pointerup", up);
          document.body.style.cursor = "";
          document.body.style.userSelect = "";
        };
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        el.addEventListener("pointermove", move);
        el.addEventListener("pointerup", up);
      }}
      onDoubleClick={() => onWidth(448)}
      className="absolute inset-y-0 -left-1 z-10 hidden w-2 cursor-col-resize hover:bg-bb-accent-strong/30 focus:bg-bb-accent-strong/30 md:block"
    />
  );
}

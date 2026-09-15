import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { api } from "./api";
import { MeContext } from "./me";
import { go, useRoute, type Route } from "./route";
import { useLoad, useMemberIndex, usePresence, useProject, useUnread } from "./store";
import type { Message, Person, Project } from "./types";
import { ChannelHeader, ChannelView, MenuButton } from "./ui/Channel";
import { InboxBell } from "./ui/Inbox";
import { ProjectCtx, type Ctx } from "./ui/context";
import { Button, Empty, ErrorNote, cx, inputClass } from "./ui/kit";
import { Decisions, Settings, UsageView } from "./ui/Settings";
import { NewProject, Sidebar, dmName } from "./ui/Sidebar";
import { Board, TaskPage } from "./ui/Tasks";
import { ThreadPanel } from "./ui/Thread";

export default function App() {
  const [me, setMe] = useState<Person | null | undefined>(undefined);
  useEffect(() => {
    api.me().then((r) => setMe(r.person), () => setMe(null));
  }, []);
  if (me === undefined) return <Splash>Connecting…</Splash>;
  if (me === null) return <NamePrompt onDone={setMe} />;
  return (
    <MeContext.Provider value={me}>
      <Root me={me} />
    </MeContext.Provider>
  );
}

function Splash({ children }: { children: ReactNode }) {
  return <main className="flex h-dvh items-center justify-center text-[13px] text-bb-subtle">{children}</main>;
}

function NamePrompt({ onDone }: { onDone: (p: Person) => void }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      onDone((await api.setMe(name.trim())).person);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }
  return (
    <main className="flex h-dvh items-center justify-center px-4">
      <form onSubmit={submit} className="w-full max-w-sm space-y-4">
        <div className="flex items-center gap-2.5">
          <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-bb-accent-strong text-[17px] font-bold text-stone-950">B</span>
          <h1 className="text-[20px] font-semibold">BuildBee</h1>
        </div>
        <p className="text-[13.5px] text-bb-muted">People and coding agents, working on your Projects together. What should we call you?</p>
        <input className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Your name" />
        <ErrorNote>{error}</ErrorNote>
        <Button tone="primary" type="submit" disabled={!name.trim()} className="w-full">
          Continue
        </Button>
        <p className="text-[12px] text-bb-subtle">No password on this network: your name attributes your work.</p>
      </form>
    </main>
  );
}

/** Root picks the Project from the address, or asks for one. */
function Root({ me }: { me: Person }) {
  const route = useRoute();
  const { data: projects, reload } = useLoad<Project[]>(() => api.projects().then((r) => r.items), []);
  const projectId = route.view !== "home" && route.view !== "usage" ? route.projectId : route.view === "usage" ? route.projectId : undefined;
  useEffect(() => {
    if (!projects || projectId || route.view === "usage") return;
    const last = localStorage.getItem("buildbee.project");
    const pick = projects.find((p) => p.id === last) ?? projects[0];
    if (pick) go({ view: "channel", projectId: pick.id });
  }, [projects, projectId, route.view]);
  useEffect(() => {
    if (projectId) localStorage.setItem("buildbee.project", projectId);
  }, [projectId]);
  if (!projects) return <Splash>Loading…</Splash>;
  if (!projectId) {
    if (route.view === "usage") return <Workspace me={me} route={route} projects={projects} projectId={projects[0]?.id} />;
    return <Welcome onCreated={reload} hasProjects={projects.length > 0} />;
  }
  return <Workspace me={me} route={route} projects={projects} projectId={projectId} />;
}

function Welcome({ onCreated, hasProjects }: { onCreated: () => void; hasProjects: boolean }) {
  const [creating, setCreating] = useState(!hasProjects);
  return (
    <main className="flex h-dvh items-center justify-center px-4">
      <Empty title="Start a Project">
        <p>A Project is a repo plus the people and Bots who work on it.</p>
        <Button tone="primary" className="mt-4" onClick={() => setCreating(true)}>
          New Project
        </Button>
      </Empty>
      {creating && (
        <NewProject
          onClose={() => {
            setCreating(false);
            onCreated();
          }}
        />
      )}
    </main>
  );
}

/** Workspace is one Project: sidebar, the current view, and a thread beside it. */
function Workspace({ me, route, projects, projectId }: { me: Person; route: Route; projects: Project[]; projectId?: string }) {
  const { data, error, reload } = useProject(projectId);
  const presence = usePresence();
  const members = useMemberIndex(data?.members);
  const tasks = useMemo(() => new Map((data?.tasks ?? []).map((t) => [t.id, t])), [data?.tasks]);
  const online = useMemo(() => new Set((presence?.people ?? []).map((p) => p.id)), [presence]);
  const myMember = data?.members.find((m) => m.person_id === me.id);
  const [drawer, setDrawer] = useState(false);
  const channelId = route.view === "channel" ? route.channelId ?? data?.channels[0]?.id : undefined;
  const allChannels = [...(data?.channels ?? []), ...(data?.dms ?? [])];
  const unread = useUnread(projectId, allChannels.map((c) => c.id), channelId, myMember?.id);
  const threadId = "threadId" in route ? route.threadId : undefined;

  if (!data) return <Splash>{error ? <ErrorNote>{error}</ErrorNote> : "Loading…"}</Splash>;
  const ctx: Ctx = { me, data, members, tasks, online, myMember, reload };
  const openThread = (m: Message | string) => {
    const id = typeof m === "string" ? m : m.id;
    if (route.view === "channel") go({ ...route, channelId, threadId: id });
    else go({ view: "channel", projectId: data.project.id, channelId: typeof m === "string" ? undefined : m.channel_id, threadId: id });
  };
  const closeThread = () => route.view === "channel" && go({ ...route, threadId: undefined });
  const menu = () => setDrawer(true);
  const channel = allChannels.find((c) => c.id === channelId);
  const titled = (title: string) => <TopBar title={title} onMenu={menu} />;

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
      main = <UsageView header={titled("Usage")} projectId={route.projectId} />;
      break;
    default:
      main = channel ? (
        <ChannelView
          key={channel.id}
          channel={channel.kind === "dm" ? { ...channel, name: dmName(channel, members, myMember?.id) } : channel}
          onOpenThread={openThread}
          header={<ChannelHeader channel={channel.kind === "dm" ? { ...channel, name: dmName(channel, members, myMember?.id) } : channel} onMenu={menu} />}
        />
      ) : (
        <Empty title="No channel">Create one from the sidebar.</Empty>
      );
  }

  return (
    <ProjectCtx.Provider value={ctx}>
      <div className="flex h-dvh overflow-hidden">
        {/* sidebar: fixed on wide screens, a drawer on phones */}
        <div className={cx("fixed inset-0 z-40 bg-black/40 lg:hidden", drawer ? "block" : "hidden")} onClick={() => setDrawer(false)} />
        <aside
          className={cx(
            "fixed inset-y-0 left-0 z-50 w-72 border-r border-bb-border transition-transform lg:static lg:z-auto lg:w-64 lg:translate-x-0",
            drawer ? "translate-x-0" : "-translate-x-full",
          )}
        >
          <Sidebar route={route} projects={projects} unread={unread} presence={presence} onNavigate={() => setDrawer(false)} />
        </aside>
        <main className="flex min-w-0 flex-1">
          <div className={cx("min-w-0 flex-1", threadId && "hidden md:block")}>{main}</div>
          {threadId && (
            <div className="fixed inset-0 z-30 md:static md:z-auto md:w-[26rem] md:shrink-0 xl:w-[30rem]">
              <ThreadPanel key={threadId} rootId={threadId} onClose={closeThread} />
            </div>
          )}
        </main>
      </div>
    </ProjectCtx.Provider>
  );
}

function TopBar({ title, onMenu }: { title: string; onMenu: () => void }) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-bb-border px-4">
      <MenuButton onClick={onMenu} />
      <h1 className="text-[15px] font-semibold">{title}</h1>
      <div className="ml-auto">
        <InboxBell />
      </div>
    </header>
  );
}

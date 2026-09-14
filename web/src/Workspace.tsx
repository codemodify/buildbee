import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { api } from "./api";
import { projectHash, type Route } from "./hash";
import { ProjectPane } from "./ProjectPane";
import type { Channel, Project } from "./types";
import { formatError } from "./ui";
import { loadSavedPanes, loadTreeCollapsed, savePanes, saveTreeCollapsed } from "./workspaceState";

type Layout = {
  leftId?: string;
  rightId?: string;
  focus: "left" | "right";
  channels: Record<string, string | undefined>;
};

const emptyLayout = (): Layout => ({
  focus: "left",
  channels: {},
});

function knownId(items: Project[], id?: string): string | undefined {
  if (!id) return undefined;
  return items.some((p) => p.id === id) ? id : undefined;
}

function layoutFromRoute(route: Route, items: Project[], prev: Layout, initial: boolean): Layout {
  const next: Layout = {
    leftId: knownId(items, prev.leftId),
    rightId: knownId(items, prev.rightId),
    focus: prev.focus,
    channels: { ...prev.channels },
  };
  if (next.rightId && next.rightId === next.leftId) next.rightId = undefined;
  if (next.focus === "right" && !next.rightId) next.focus = "left";

  if (route.page === "project") {
    const primary = knownId(items, route.projectId);
    const beside = knownId(items, route.besideId);
    if (primary) {
      if (route.channelId) next.channels[primary] = route.channelId;
      if (next.leftId === primary && next.rightId === beside) {
        next.focus = "left";
      } else if (next.leftId === beside && next.rightId === primary) {
        next.focus = "right";
      } else if (next.leftId === primary) {
        next.focus = "left";
        if (beside) next.rightId = beside;
      } else if (next.rightId === primary) {
        next.focus = "right";
        if (beside && next.leftId !== beside) next.leftId = beside;
      } else {
        next.leftId = primary;
        next.rightId = beside;
        next.focus = "left";
      }
    }
    return next;
  }

  if (route.page === "task") {
    const pid = knownId(items, route.projectId);
    if (pid) {
      if (next.leftId !== pid && next.rightId !== pid) {
        next.leftId = pid;
        next.focus = "left";
      } else {
        next.focus = next.rightId === pid ? "right" : "left";
      }
    }
    return next;
  }

  if (route.page === "home" && items.length) {
    if (initial) {
      const saved = loadSavedPanes();
      const left = knownId(items, saved?.left) ?? items[0].id;
      const right = knownId(items, saved?.right);
      next.leftId = left;
      next.rightId = right && right !== left ? right : undefined;
      next.focus = saved?.focus === "right" && next.rightId ? "right" : "left";
      if (saved?.channels) {
        for (const [id, ch] of Object.entries(saved.channels)) {
          if (ch) next.channels[id] = ch;
        }
      }
    } else if (!next.leftId) {
      next.leftId = items[0].id;
      next.focus = "left";
    }
  }
  return next;
}

function persist(layout: Layout) {
  const channels: Record<string, string> = {};
  for (const [id, ch] of Object.entries(layout.channels)) {
    if (ch) channels[id] = ch;
  }
  savePanes({
    left: layout.leftId,
    right: layout.rightId,
    focus: layout.focus,
    channels,
  });
}

function layoutsEqual(a: Layout, b: Layout): boolean {
  if (a.leftId !== b.leftId || a.rightId !== b.rightId || a.focus !== b.focus) return false;
  const keys = new Set([...Object.keys(a.channels), ...Object.keys(b.channels)]);
  for (const k of keys) {
    if (a.channels[k] !== b.channels[k]) return false;
  }
  return true;
}

function applyLayout(prev: Layout, route: Route, items: Project[], initial: boolean): Layout {
  const next = layoutFromRoute(route, items, prev, initial);
  return layoutsEqual(prev, next) ? prev : next;
}

export function Workspace({ route, children }: { route: Route; children?: ReactNode }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [layout, setLayout] = useState<Layout>(emptyLayout);
  const [loaded, setLoaded] = useState(false);
  const [channelsVersion, setChannelsVersion] = useState(0);
  const skipHash = useRef(false);
  const projectsRef = useRef(projects);
  const routeRef = useRef(route);
  projectsRef.current = projects;
  routeRef.current = route;

  async function refreshProjects(): Promise<Project[]> {
    const data = await api.listProjects();
    setProjects(data.items);
    return data.items;
  }

  useEffect(() => {
    void (async () => {
      const items = await refreshProjects().catch(() => [] as Project[]);
      setLayout((prev) => applyLayout(prev, routeRef.current, items, true));
      setLoaded(true);
    })();
  }, []);

  useEffect(() => {
    if (!loaded) return;
    if (skipHash.current) {
      skipHash.current = false;
      return;
    }
    setLayout((prev) => applyLayout(prev, route, projectsRef.current, false));
  }, [route, loaded]);

  useEffect(() => {
    if (!loaded || projects.length === 0) return;
    persist(layout);
    if (route.page === "task" || route.page === "settings" || route.page === "invite") return;
    const focusId =
      layout.focus === "right" && layout.rightId ? layout.rightId : layout.leftId;
    if (!focusId) return;
    const besideId = focusId === layout.leftId ? layout.rightId : layout.leftId;
    const next = projectHash({
      projectId: focusId,
      channelId: layout.channels[focusId],
      besideId,
    });
    if (window.location.hash !== next) {
      skipHash.current = true;
      window.location.hash = next;
    }
  }, [layout, loaded, projects.length, route.page]);

  function openProject(id: string, mode: "focus" | "beside") {
    setLayout((cur) => {
      if (cur.leftId === id) return { ...cur, focus: "left" };
      if (cur.rightId === id) return { ...cur, focus: "right" };
      if (mode === "beside") {
        if (!cur.leftId) return { ...cur, leftId: id, focus: "left" };
        if (!cur.rightId) return { ...cur, rightId: id, focus: "right" };
        if (cur.focus === "left") return { ...cur, rightId: id, focus: "right" };
        return { ...cur, leftId: id, focus: "left" };
      }
      if (!cur.leftId) return { ...cur, leftId: id, focus: "left" };
      if (cur.focus === "right" && cur.rightId) {
        return { ...cur, rightId: id, focus: "right" };
      }
      return { ...cur, leftId: id, focus: "left" };
    });
  }

  function closePane(side: "left" | "right") {
    setLayout((cur) => {
      if (side === "left") {
        if (cur.rightId) {
          return { ...cur, leftId: cur.rightId, rightId: undefined, focus: "left" };
        }
        return { ...cur, leftId: undefined, focus: "left" };
      }
      return { ...cur, rightId: undefined, focus: "left" };
    });
  }

  function selectChannel(projectId: string, channelId: string) {
    setLayout((cur) => {
      const channels = { ...cur.channels, [projectId]: channelId };
      if (cur.leftId === projectId) return { ...cur, channels, focus: "left" };
      if (cur.rightId === projectId) return { ...cur, channels, focus: "right" };
      if (!cur.leftId) return { ...cur, channels, leftId: projectId, focus: "left" };
      if (cur.focus === "right" && cur.rightId) {
        return { ...cur, channels, rightId: projectId, focus: "right" };
      }
      return { ...cur, channels, leftId: projectId, focus: "left" };
    });
  }

  async function onCreated(project: Project) {
    const items = await refreshProjects();
    setLayout((cur) => {
      if (cur.leftId && !cur.rightId && items.length > 1) {
        return { ...cur, rightId: project.id, focus: "right" };
      }
      return { ...cur, leftId: project.id, rightId: cur.rightId, focus: "left" };
    });
  }

  if (!loaded) {
    return (
      <div className="flex h-full min-h-0 flex-1 items-center justify-center text-sm text-zinc-500">
        Loading Projects…
      </div>
    );
  }

  if (projects.length === 0) {
    if (children) {
      return <div className="min-h-0 flex-1 overflow-auto">{children}</div>;
    }
    return <EmptyHome onCreated={onCreated} />;
  }

  const split = Boolean(layout.leftId && layout.rightId);
  const showClose = split;

  return (
    <div className="flex h-full min-h-0 w-full flex-1 overflow-hidden">
      <ProjectsRail
        projects={projects}
        layout={layout}
        onOpen={openProject}
        onSelectChannel={selectChannel}
        onChannelCreated={() => setChannelsVersion((n) => n + 1)}
        onCreated={onCreated}
      />
      {children ? (
        <div className="min-h-0 flex-1 overflow-auto">{children}</div>
      ) : (
        <div className="flex min-h-0 min-w-0 flex-1">
          {layout.leftId ? (
            <div className="min-h-0 min-w-0 flex-1">
              <ProjectPane
                key={`left-${layout.leftId}`}
                projectId={layout.leftId}
                channelId={layout.channels[layout.leftId]}
                focused={layout.focus === "left"}
                compact={split}
                showClose={showClose}
                channelsVersion={channelsVersion}
                onFocus={() => setLayout((c) => ({ ...c, focus: "left" }))}
                onClose={() => closePane("left")}
              />
            </div>
          ) : null}
          {layout.rightId ? (
            <div className="min-h-0 min-w-0 flex-1 border-l border-zinc-800">
              <ProjectPane
                key={`right-${layout.rightId}`}
                projectId={layout.rightId}
                channelId={layout.channels[layout.rightId]}
                focused={layout.focus === "right"}
                compact={split}
                showClose={showClose}
                channelsVersion={channelsVersion}
                onFocus={() => setLayout((c) => ({ ...c, focus: "right" }))}
                onClose={() => closePane("right")}
              />
            </div>
          ) : null}
          {!layout.leftId && !layout.rightId ? (
            <div className="flex flex-1 items-center justify-center text-sm text-zinc-500">
              Pick a Project in the rail.
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

function ProjectsRail({
  projects,
  layout,
  onOpen,
  onSelectChannel,
  onChannelCreated,
  onCreated,
}: {
  projects: Project[];
  layout: Layout;
  onOpen: (id: string, mode: "focus" | "beside") => void;
  onSelectChannel: (projectId: string, channelId: string) => void;
  onChannelCreated: () => void;
  onCreated: (project: Project) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [channelsByProject, setChannelsByProject] = useState<Record<string, Channel[]>>({});
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(loadTreeCollapsed);
  const [draftByProject, setDraftByProject] = useState<Record<string, string>>({});
  const canCreate = name.trim().length > 0;
  const projectKey = projects.map((p) => p.id).join(",");

  useEffect(() => {
    saveTreeCollapsed(collapsed);
  }, [collapsed]);

  useEffect(() => {
    let stop = false;
    void (async () => {
      const next: Record<string, Channel[]> = {};
      await Promise.all(
        projects.map(async (p) => {
          try {
            next[p.id] = (await api.listChannels(p.id)).items;
          } catch {
            next[p.id] = [];
          }
        }),
      );
      if (!stop) setChannelsByProject(next);
    })();
    return () => {
      stop = true;
    };
  }, [projectKey]);

  function isCollapsed(id: string): boolean {
    return collapsed[id] === true;
  }

  function toggleCollapsed(id: string) {
    setCollapsed((cur) => ({ ...cur, [id]: !cur[id] }));
  }

  function expand(id: string) {
    setCollapsed((cur) => (cur[id] ? { ...cur, [id]: false } : cur));
  }

  async function refreshChannels(projectId: string) {
    try {
      const data = await api.listChannels(projectId);
      setChannelsByProject((cur) => ({ ...cur, [projectId]: data.items }));
    } catch {
      /* keep last */
    }
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    const title = name.trim();
    if (!title) return;
    setError("");
    try {
      const p = await api.createProject(title);
      setName("");
      setCollapsed((cur) => ({ ...cur, [p.id]: false }));
      await onCreated(p);
      await refreshChannels(p.id);
    } catch (err) {
      setError(formatError(err));
    }
  }

  async function onCreateChannel(projectId: string, e: FormEvent) {
    e.preventDefault();
    const title = (draftByProject[projectId] ?? "").trim();
    if (!title) return;
    setError("");
    try {
      const ch = await api.createChannel(projectId, title);
      setDraftByProject((cur) => ({ ...cur, [projectId]: "" }));
      expand(projectId);
      await refreshChannels(projectId);
      onChannelCreated();
      onSelectChannel(projectId, ch.id);
    } catch (err) {
      setError(formatError(err));
    }
  }

  return (
    <aside
      data-testid="projects-rail"
      className="flex w-60 shrink-0 flex-col border-r border-zinc-800 bg-zinc-950"
    >
      <div className="border-b border-zinc-800 px-3 py-2">
        <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">Projects</p>
        <p className="mt-1 text-[11px] leading-snug text-zinc-500">
          Chevron expands. Click name to focus. Alt-click or ⊕ opens beside.
        </p>
      </div>
      <ul className="min-h-0 flex-1 space-y-1 overflow-auto p-1">
        {projects.map((p) => {
          const inLeft = layout.leftId === p.id;
          const inRight = layout.rightId === p.id;
          const open = inLeft || inRight;
          const focused =
            (inLeft && layout.focus === "left") || (inRight && layout.focus === "right");
          const expanded = !isCollapsed(p.id);
          const children = channelsByProject[p.id] ?? [];
          const activeId = layout.channels[p.id] ?? children[0]?.id;
          return (
            <li key={p.id}>
              <div
                className={`flex items-stretch rounded ${
                  focused
                    ? "bg-amber-400/15 ring-1 ring-amber-400/60"
                    : open
                      ? "bg-zinc-800/80"
                      : "hover:bg-zinc-900"
                }`}
              >
                <button
                  type="button"
                  data-testid="project-rail-chevron"
                  aria-label={expanded ? `Collapse ${p.name}` : `Expand ${p.name}`}
                  aria-expanded={expanded}
                  className="w-6 shrink-0 text-center text-xs text-zinc-400 hover:text-zinc-100"
                  onClick={() => toggleCollapsed(p.id)}
                >
                  {expanded ? "▾" : "▸"}
                </button>
                <button
                  type="button"
                  data-testid="project-rail-row"
                  data-project-id={p.id}
                  data-open={open ? "true" : "false"}
                  data-focused={focused ? "true" : "false"}
                  className="min-w-0 flex-1 truncate py-1.5 pr-1 text-left text-sm text-zinc-100"
                  onClick={(e) => onOpen(p.id, e.altKey ? "beside" : "focus")}
                >
                  {p.name}
                </button>
                <button
                  type="button"
                  data-testid="open-beside"
                  title="Open beside"
                  aria-label={`Open ${p.name} beside`}
                  className="px-1.5 text-xs text-zinc-400 hover:text-amber-300"
                  onClick={() => onOpen(p.id, "beside")}
                >
                  ⊕
                </button>
              </div>
              {expanded ? (
                <ul className="mt-0.5 space-y-0.5 pl-6">
                  {children.map((ch) => {
                    const treeActive = focused && activeId === ch.id;
                    const openQuiet = open && !focused && activeId === ch.id;
                    return (
                      <li key={ch.id}>
                        <button
                          type="button"
                          data-testid="rail-channel"
                          data-channel-id={ch.id}
                          data-active={treeActive ? "true" : "false"}
                          className={`block w-full truncate rounded px-2 py-1 text-left text-sm ${
                            treeActive
                              ? "bg-amber-400/15 text-amber-200"
                              : openQuiet
                                ? "bg-zinc-800/60 text-zinc-200"
                                : "text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"
                          }`}
                          onClick={() => {
                            expand(p.id);
                            onSelectChannel(p.id, ch.id);
                          }}
                        >
                          #{ch.name}
                        </button>
                      </li>
                    );
                  })}
                  <li>
                    <form
                      className="flex gap-1 px-1 pt-0.5"
                      onSubmit={(e) => void onCreateChannel(p.id, e)}
                    >
                      <input
                        className="min-w-0 flex-1 rounded border border-zinc-800 bg-zinc-900 px-1.5 py-0.5 text-[11px] text-zinc-100"
                        value={draftByProject[p.id] ?? ""}
                        onChange={(e) =>
                          setDraftByProject((cur) => ({ ...cur, [p.id]: e.target.value }))
                        }
                        placeholder="new channel"
                        aria-label={`New Channel in ${p.name}`}
                      />
                      <button
                        type="submit"
                        className="rounded bg-zinc-800 px-1.5 text-[11px] text-zinc-300 hover:text-zinc-100"
                        aria-label={`Create Channel in ${p.name}`}
                      >
                        +
                      </button>
                    </form>
                  </li>
                </ul>
              ) : null}
            </li>
          );
        })}
      </ul>
      {error ? <p className="px-2 text-[11px] text-red-400">{error}</p> : null}
      <form onSubmit={onCreate} className="space-y-1 border-t border-zinc-800 p-2">
        <input
          className="w-full rounded border border-zinc-800 bg-zinc-900 px-2 py-1 text-xs text-zinc-100"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="project name"
        />
        <button
          type="submit"
          disabled={!canCreate}
          className="w-full rounded bg-amber-400 px-2 py-1 text-xs font-medium text-zinc-950 disabled:opacity-50"
        >
          create
        </button>
      </form>
    </aside>
  );
}

function EmptyHome({ onCreated }: { onCreated: (project: Project) => Promise<void> }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const canCreate = name.trim().length > 0;

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    const title = name.trim();
    if (!title) return;
    setError("");
    try {
      const p = await api.createProject(title);
      await onCreated(p);
    } catch (err) {
      setError(formatError(err));
    }
  }

  return (
    <main className="flex h-full min-h-0 w-full flex-1 flex-col items-center justify-center overflow-hidden bg-zinc-950 px-6 text-zinc-100">
      <div className="w-full max-w-xl">
        <form onSubmit={onCreate} className="flex gap-2">
          <input
            className="flex-1 rounded-md border border-zinc-800 bg-zinc-900 px-3 py-2"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="project name"
          />
          <button
            type="submit"
            disabled={!canCreate}
            className="rounded-md bg-amber-400 px-4 py-2 font-medium text-zinc-950 disabled:opacity-50"
          >
            create
          </button>
        </form>
        {error ? <p className="mt-3 text-sm text-red-400">{error}</p> : null}
      </div>
    </main>
  );
}

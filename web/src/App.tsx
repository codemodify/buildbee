import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";

function renderMentions(text: string): ReactNode {
  const parts = text.split(/(@[A-Za-z0-9_-]+)/g);
  return parts.map((part, i) =>
    part.startsWith("@") ? (
      <span key={i} className="font-medium text-amber-200">
        {part}
      </span>
    ) : (
      <span key={i}>{part}</span>
    ),
  );
}
import { api } from "./api";
import type {
  AuthMe,
  Channel,
  Decision,
  Member,
  Message,
  Project,
  Routine,
  Task,
  TaskDetail,
} from "./types";

type Route =
  | { page: "home" }
  | { page: "project"; projectId: string; channelId?: string }
  | { page: "task"; projectId: string; taskId: string };

function parseHash(): Route {
  const raw = window.location.hash.replace(/^#/, "");
  const parts = raw.split("/").filter(Boolean);
  if (parts[0] === "projects" && parts[1] && parts[2] === "tasks" && parts[3]) {
    return { page: "task", projectId: parts[1], taskId: parts[3] };
  }
  if (parts[0] === "projects" && parts[1]) {
    return {
      page: "project",
      projectId: parts[1],
      channelId: parts[2] === "channels" ? parts[3] : undefined,
    };
  }
  return { page: "home" };
}

export default function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  useEffect(() => {
    const onHash = () => setRoute(parseHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  if (route.page === "task") {
    return (
      <>
        <AuthBar />
        <TaskPage projectId={route.projectId} taskId={route.taskId} />
      </>
    );
  }
  if (route.page === "project") {
    return (
      <>
        <AuthBar />
        <ProjectPage projectId={route.projectId} channelId={route.channelId} />
      </>
    );
  }
  return (
    <>
      <AuthBar />
      <HomePage />
    </>
  );
}

function AuthBar() {
  const [me, setMe] = useState<AuthMe | null>(null);
  useEffect(() => {
    void api.authMe().then(setMe).catch(() => setMe(null));
  }, []);
  if (!me) return null;
  if (me.dev) {
    return (
      <div className="bg-amber-400/15 px-4 py-2 text-center text-sm text-amber-200">
        Dev auth: acting as Member {me.identity?.display_name ?? "You"} (set
        GITHUB_CLIENT_ID to enable GitHub OAuth)
      </div>
    );
  }
  return (
    <div className="flex items-center justify-end gap-3 bg-zinc-900 px-4 py-2 text-sm">
      {me.signed_in ? (
        <span>Signed in as {me.identity?.github_login ?? me.identity?.display_name}</span>
      ) : (
        <a
          href="/v1/auth/github"
          className="rounded bg-zinc-100 px-3 py-1 font-medium text-zinc-950"
        >
          Sign in with GitHub
        </a>
      )}
    </div>
  );
}

function HomePage() {
  const [name, setName] = useState("Hive");
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([]);
  const [error, setError] = useState("");

  async function refresh() {
    try {
      const data = await api.listProjects();
      setProjects(data.items);
    } catch (e) {
      setError(String(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const p = await api.createProject(name.trim());
      window.location.hash = `#/projects/${p.id}`;
    } catch (err) {
      setError(String(err));
    }
  }

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-16 text-zinc-100">
      <div className="mx-auto max-w-xl">
        <p className="text-sm font-medium tracking-wide text-amber-400">
          Project workspace
        </p>
        <h1 className="mt-2 text-4xl font-semibold">BuildBee</h1>
        <p className="mt-3 text-zinc-400">
          Create a Project, then chat in a Channel, open Tasks, and answer
          Decisions.
        </p>
        <form onSubmit={onCreate} className="mt-8 flex gap-2">
          <input
            className="flex-1 rounded-md border border-zinc-800 bg-zinc-900 px-3 py-2"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Project name"
          />
          <button
            type="submit"
            className="rounded-md bg-amber-400 px-4 py-2 font-medium text-zinc-950"
          >
            Create
          </button>
        </form>
        {error ? <p className="mt-3 text-sm text-red-400">{error}</p> : null}
        <ul className="mt-10 space-y-2">
          {projects.map((p) => (
            <li key={p.id}>
              <a
                href={`#/projects/${p.id}`}
                className="block rounded-md border border-zinc-800 bg-zinc-900 px-4 py-3 hover:border-zinc-600"
              >
                {p.name}
              </a>
            </li>
          ))}
        </ul>
      </div>
    </main>
  );
}

function ProjectPage({
  projectId,
  channelId,
}: {
  projectId: string;
  channelId?: string;
}) {
  const [project, setProject] = useState<Project | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [body, setBody] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const [channelName, setChannelName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [options, setOptions] = useState("yes, no");
  const [memberName, setMemberName] = useState("");
  const [memberKind, setMemberKind] = useState("human");
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [handoffRole, setHandoffRole] = useState("scout");
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([]);
  const [error, setError] = useState("");

  const activeChannel = useMemo(() => {
    if (channelId) return channels.find((c) => c.id === channelId) ?? channels[0];
    return channels[0];
  }, [channels, channelId]);

  const human = members.find((m) => m.kind === "human");
  const bots = members.filter((m) => m.kind === "bot");

  async function loadProject() {
    const p = await api.getProject(projectId);
    setProject(p);
    const [m, c, t, d, rts, plist] = await Promise.all([
      api.listMembers(projectId),
      api.listChannels(projectId),
      api.listTasks(projectId),
      api.listDecisions(projectId),
      api.listRoutines(projectId),
      api.listProjects(),
    ]);
    setProjects(plist.items);
    setMembers(m.items);
    setChannels(c.items);
    setTasks(t.items);
    setDecisions(d.items);
    setRoutines(rts.items);
    return c.items;
  }

  useEffect(() => {
    void loadProject().catch((e) => setError(String(e)));
  }, [projectId]);

  useEffect(() => {
    if (!activeChannel) return;
    const channelKey = activeChannel.id;
    let stop = false;
    async function tick() {
      try {
        const data = await api.listMessages(channelKey);
        if (!stop) setMessages(data.items);
      } catch {
        /* keep last list */
      }
    }
    void tick();
    const id = window.setInterval(() => void tick(), 2000);
    return () => {
      stop = true;
      window.clearInterval(id);
    };
  }, [activeChannel?.id]);

  async function send(e: FormEvent) {
    e.preventDefault();
    if (!activeChannel || !human || !body.trim()) return;
    await api.postMessage(activeChannel.id, body.trim(), human.id);
    setBody("");
    const data = await api.listMessages(activeChannel.id);
    setMessages(data.items);
  }

  async function addTask(e: FormEvent) {
    e.preventDefault();
    if (!taskTitle.trim()) return;
    await api.createTask(projectId, taskTitle.trim(), handoffRole);
    setTaskTitle("");
    setTasks((await api.listTasks(projectId)).items);
  }

  async function addChannel(e: FormEvent) {
    e.preventDefault();
    if (!channelName.trim()) return;
    const ch = await api.createChannel(projectId, channelName.trim());
    setChannelName("");
    setChannels((prev) => [...prev, ch]);
    window.location.hash = `#/projects/${projectId}/channels/${ch.id}`;
  }

  async function addMember(e: FormEvent) {
    e.preventDefault();
    if (!memberName.trim()) return;
    await api.addMember(projectId, {
      display_name: memberName.trim(),
      kind: memberKind,
    });
    setMemberName("");
    setMembers((await api.listMembers(projectId)).items);
  }

  async function addDecision(e: FormEvent) {
    e.preventDefault();
    if (!prompt.trim()) return;
    const opts = options
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    await api.createDecision(projectId, {
      prompt: prompt.trim(),
      options: opts,
      recommendation: opts[0] ?? "",
    });
    setPrompt("");
    const d = await api.listDecisions(projectId);
    setDecisions(d.items);
  }

  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100">
      <header className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
        <div>
          <a href="#/" className="text-sm text-amber-400">
            BuildBee
          </a>
          <h1 className="text-xl font-semibold">{project?.name ?? "Project"}</h1>
          {projects.length > 1 ? (
            <select
              className="mt-1 rounded border border-zinc-800 bg-zinc-950 text-xs"
              value={projectId}
              onChange={(e) => {
                window.location.hash = `#/projects/${e.target.value}`;
              }}
              aria-label="Switch Project"
            >
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          ) : null}
        </div>
        <form onSubmit={addMember} className="flex items-center gap-2 text-xs">
          <label className="flex items-center gap-1 text-zinc-400">
            <input
              type="checkbox"
              checked={!!project?.auto_run}
              onChange={(e) => {
                const on = e.target.checked;
                void api.updateProject(projectId, { auto_run: on }).then((p) => setProject(p));
              }}
            />
            auto_run
          </label>
          <span className="text-zinc-500">
            {members.length} Members
          </span>
          <input
            className="w-32 rounded border border-zinc-800 bg-zinc-950 px-2 py-1"
            value={memberName}
            onChange={(e) => setMemberName(e.target.value)}
            placeholder="name"
          />
          <select
            className="bg-zinc-950"
            value={memberKind}
            onChange={(e) => setMemberKind(e.target.value)}
          >
            <option value="human">human</option>
            <option value="bot">bot</option>
          </select>
          <button type="submit" className="rounded bg-zinc-800 px-2 py-1">
            Add Member
          </button>
        </form>
      </header>
      {error ? <p className="px-4 py-2 text-sm text-red-400">{error}</p> : null}
      <div className="grid min-h-[calc(100vh-4rem)] gap-4 p-4 lg:grid-cols-[14rem_minmax(0,1fr)_18rem]">
        <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
          <h2 className="text-sm font-medium text-zinc-400">Channels</h2>
          <ul className="mt-2 space-y-1">
            {channels.map((ch) => (
              <li key={ch.id}>
                <a
                  href={`#/projects/${projectId}/channels/${ch.id}`}
                  className={`block rounded px-2 py-1 text-sm ${
                    activeChannel?.id === ch.id
                      ? "bg-zinc-800 text-amber-300"
                      : "text-zinc-300 hover:bg-zinc-800"
                  }`}
                >
                  #{ch.name}
                </a>
              </li>
            ))}
          </ul>
          <form onSubmit={addChannel} className="mt-3 flex gap-1">
            <input
              className="min-w-0 flex-1 rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
              value={channelName}
              onChange={(e) => setChannelName(e.target.value)}
              placeholder="new channel"
            />
            <button className="rounded bg-zinc-800 px-2 text-sm" type="submit">
              +
            </button>
          </form>
        </section>

        <section className="flex min-h-[24rem] flex-col rounded-lg border border-zinc-800 bg-zinc-900/40">
          <div className="border-b border-zinc-800 px-3 py-2 text-sm text-zinc-400">
            #{activeChannel?.name ?? "…"}
          </div>
          <ul className="flex-1 space-y-2 overflow-auto p-3">
            {messages.map((m) => {
              const who =
                members.find((mem) => mem.id === m.member_id)?.display_name ??
                m.member_id;
              return (
                <li key={m.id} className="text-sm">
                  <span className="font-medium text-amber-300">{who}</span>{" "}
                  <span className="text-zinc-200">{renderMentions(m.body)}</span>
                </li>
              );
            })}
          </ul>
          <form onSubmit={send} className="flex gap-2 border-t border-zinc-800 p-3">
            <input
              className="flex-1 rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder="Message the Channel"
            />
            <button
              type="submit"
              className="rounded-md bg-amber-400 px-3 py-2 text-sm font-medium text-zinc-950"
            >
              Send
            </button>
          </form>
        </section>

        <div className="space-y-4">
          <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
            <h2 className="flex items-center justify-between text-sm font-medium text-zinc-400">
              Tasks
              <button
                type="button"
                className="rounded bg-zinc-800 px-2 py-0.5 text-xs text-zinc-200"
                onClick={() => {
                  void api.syncIssues(projectId).then(async () => {
                    setTasks((await api.listTasks(projectId)).items);
                  });
                }}
              >
                Sync Issues
              </button>
            </h2>
            <form onSubmit={addTask} className="mt-2 flex flex-wrap gap-1">
              <input
                className="min-w-0 flex-1 rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
                value={taskTitle}
                onChange={(e) => setTaskTitle(e.target.value)}
                placeholder="New Task"
              />
              <select
                className="rounded border border-zinc-800 bg-zinc-950 px-1 text-xs"
                value={handoffRole}
                onChange={(e) => setHandoffRole(e.target.value)}
                aria-label="Handoff Role"
              >
                <option value="scout">Handoff → Scout</option>
                <option value="builder">Handoff → Builder</option>
                <option value="none">No Handoff</option>
              </select>
              <button className="rounded bg-amber-400 px-2 text-sm text-zinc-950" type="submit">
                Add
              </button>
            </form>
            <div className="mt-2 grid grid-cols-3 gap-1">
              {(["open", "in_progress", "done"] as const).map((col) => (
                <div key={col} className="rounded border border-zinc-800 bg-zinc-950/50 p-1">
                  <p className="px-1 text-[10px] uppercase tracking-wide text-zinc-500">{col}</p>
                  <ul className="mt-1 space-y-1">
                    {tasks
                      .filter((t) => t.status === col)
                      .map((t) => (
                        <li key={t.id} className="rounded bg-zinc-900 px-1.5 py-1 text-xs">
                          <a
                            href={`#/projects/${projectId}/tasks/${t.id}`}
                            className="block hover:text-amber-300"
                          >
                            {t.title}
                          </a>
                          {t.issue_url ? (
                            <a className="text-[10px] text-amber-500 underline" href={t.issue_url}>
                              #{t.issue_number}
                            </a>
                          ) : null}
                          <select
                            className="mt-1 w-full bg-zinc-950 text-[10px]"
                            value={t.status}
                            onChange={(e) => {
                              const status = e.target.value;
                              void api.updateTask(t.id, status).then(async () => {
                                setTasks((await api.listTasks(projectId)).items);
                              });
                            }}
                          >
                            <option value="open">open</option>
                            <option value="in_progress">in_progress</option>
                            <option value="done">done</option>
                          </select>
                        </li>
                      ))}
                  </ul>
                </div>
              ))}
            </div>
          </section>

          <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
            <h2 className="text-sm font-medium text-zinc-400">Decisions</h2>
            <form onSubmit={addDecision} className="mt-2 space-y-1">
              <input
                className="w-full rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="Decision prompt"
              />
              <input
                className="w-full rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
                value={options}
                onChange={(e) => setOptions(e.target.value)}
                placeholder="options, comma separated"
              />
              <button
                className="rounded bg-zinc-800 px-2 py-1 text-sm"
                type="submit"
              >
                Ask
              </button>
            </form>
            <ul className="mt-3 space-y-3">
              {decisions.map((d) => (
                <li key={d.id} className="text-sm">
                  <p className="font-medium">{d.prompt}</p>
                  {d.answer ? (
                    <p className="text-emerald-400">
                      Answer: {d.answer}
                      {d.reused ? (
                        <span className="ml-1 text-xs text-zinc-500">(reused memory)</span>
                      ) : null}
                    </p>
                  ) : (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {(d.options.length ? d.options : ["yes"]).map((opt) => (
                        <button
                          key={opt}
                          type="button"
                          className="rounded bg-zinc-800 px-2 py-0.5"
                          onClick={() => {
                            void api.answerDecision(d.id, opt).then(async () => {
                              setDecisions((await api.listDecisions(projectId)).items);
                            });
                          }}
                        >
                          {opt}
                        </button>
                      ))}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </section>

          <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
            <h2 className="text-sm font-medium text-zinc-400">Bots + Roles</h2>
            <ul className="mt-2 space-y-2">
              {bots.map((b) => (
                <li key={b.id} className="text-sm">
                  <span className="font-medium text-amber-300">{b.display_name}</span>{" "}
                  <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-xs uppercase text-zinc-300">
                    {b.role}
                  </span>
                  {b.instructions ? (
                    <p className="mt-0.5 text-xs text-zinc-500">{b.instructions}</p>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>

          <section className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
            <h2 className="text-sm font-medium text-zinc-400">Routines</h2>
            <ul className="mt-2 space-y-2">
              {routines.map((rt) => (
                <li key={rt.id} className="flex items-center justify-between text-sm">
                  <span>
                    {rt.name}{" "}
                    <span className="text-zinc-500">{rt.schedule}</span>
                  </span>
                  <button
                    type="button"
                    className="rounded bg-zinc-800 px-2 py-0.5 text-xs"
                    onClick={() => {
                      void api.fireRoutine(rt.id).then(async () => {
                        setTasks((await api.listTasks(projectId)).items);
                      });
                    }}
                  >
                    Run
                  </button>
                </li>
              ))}
            </ul>
          </section>
        </div>
      </div>
    </main>
  );
}

function TaskPage({ projectId, taskId }: { projectId: string; taskId: string }) {
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [toRole, setToRole] = useState("builder");
  const [note, setNote] = useState("");
  const [autorun, setAutorun] = useState(false);
  const [error, setError] = useState("");

  async function refresh() {
    const [d, m] = await Promise.all([
      api.getTaskDetail(taskId),
      api.listMembers(projectId),
    ]);
    setDetail(d);
    setMembers(m.items);
  }

  useEffect(() => {
    void refresh().catch((e) => setError(String(e)));
  }, [taskId, projectId]);

  const human = members.find((m) => m.kind === "human");
  const bots = members.filter((m) => m.kind === "bot");

  async function handoff(e: FormEvent) {
    e.preventDefault();
    if (!human) return;
    await api.createHandoff(taskId, human.id, "", note.trim() || "please take this", {
      toRole,
      autorun,
    });
    setNote("");
    await refresh();
  }

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-8 text-zinc-100">
      <a href={`#/projects/${projectId}`} className="text-sm text-amber-400">
        ← Project
      </a>
      <h1 className="mt-3 text-2xl font-semibold">{detail?.title ?? "Task"}</h1>
      <p className="text-sm text-zinc-500">status {detail?.status}</p>
      {detail?.issue_url ? (
        <p className="mt-1 text-sm">
          Issues{" "}
          <a className="text-amber-300 underline" href={detail.issue_url}>
            #{detail.issue_number} {detail.issue_url}
          </a>
        </p>
      ) : null}
      {error ? <p className="mt-2 text-sm text-red-400">{error}</p> : null}

      <form onSubmit={handoff} className="mt-6 flex flex-wrap items-end gap-2 rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
        <label className="text-xs text-zinc-400">
          Handoff to Role
          <select
            className="mt-1 block rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
            value={toRole}
            onChange={(e) => setToRole(e.target.value)}
          >
            {bots.map((b) => (
              <option key={b.id} value={b.role}>
                {b.display_name} ({b.role})
              </option>
            ))}
          </select>
        </label>
        <input
          className="min-w-[12rem] flex-1 rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Handoff notes"
        />
        <label className="flex items-center gap-1 text-xs text-zinc-400">
          <input
            type="checkbox"
            checked={autorun}
            onChange={(e) => setAutorun(e.target.checked)}
          />
          autorun (Builder)
        </label>
        <button type="submit" className="rounded bg-amber-400 px-3 py-1 text-sm text-zinc-950">
          Handoff
        </button>
      </form>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-zinc-400">Runs</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.runs ?? []).map((r) => (
            <li key={r.id} className="rounded border border-zinc-800 px-3 py-2 text-sm">
              <span className="text-amber-300">{r.status}</span> {r.detail}
            </li>
          ))}
          {detail && detail.runs.length === 0 ? (
            <li className="text-sm text-zinc-500">No Runs yet</li>
          ) : null}
        </ul>
      </section>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-zinc-400">Artifacts</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.artifacts ?? []).map((a) => (
            <li key={a.id} className="rounded border border-zinc-800 px-3 py-2 text-sm">
              <span className="text-zinc-400">{a.kind}</span> {a.name}
              {a.url ? (
                <>
                  {" "}
                  <a className="text-amber-300 underline" href={a.url}>
                    {a.url}
                  </a>
                </>
              ) : null}
              {a.body ? (
                <pre className="mt-2 max-h-40 overflow-auto text-xs text-zinc-400">
                  {a.body}
                </pre>
              ) : null}
            </li>
          ))}
        </ul>
      </section>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-zinc-400">Pipelines</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.pipelines ?? []).map((p) => (
            <li key={p.id} className="rounded border border-zinc-800 px-3 py-2 text-sm">
              <span className="text-amber-300">{p.status}</span> {p.name}
              {p.external_url ? (
                <>
                  {" "}
                  <a className="text-amber-300 underline" href={p.external_url}>
                    {p.external_url}
                  </a>
                </>
              ) : null}
            </li>
          ))}
        </ul>
      </section>
    </main>
  );
}

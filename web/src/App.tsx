import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api } from "./api";
import type { Channel, Decision, Member, Message, Project, Task } from "./types";

type Route =
  | { page: "home" }
  | { page: "project"; projectId: string; channelId?: string };

function parseHash(): Route {
  const raw = window.location.hash.replace(/^#/, "");
  const parts = raw.split("/").filter(Boolean);
  if (parts[0] === "projects" && parts[1]) {
    return { page: "project", projectId: parts[1], channelId: parts[3] };
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

  if (route.page === "project") {
    return <ProjectPage projectId={route.projectId} channelId={route.channelId} />;
  }
  return <HomePage />;
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
  const [error, setError] = useState("");

  const activeChannel = useMemo(() => {
    if (channelId) return channels.find((c) => c.id === channelId) ?? channels[0];
    return channels[0];
  }, [channels, channelId]);

  const human = members.find((m) => m.kind === "human");
  const bot = members.find((m) => m.kind === "bot");

  async function loadProject() {
    const p = await api.getProject(projectId);
    setProject(p);
    const [m, c, t, d] = await Promise.all([
      api.listMembers(projectId),
      api.listChannels(projectId),
      api.listTasks(projectId),
      api.listDecisions(projectId),
    ]);
    setMembers(m.items);
    setChannels(c.items);
    setTasks(t.items);
    setDecisions(d.items);
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
    const t = await api.createTask(projectId, taskTitle.trim());
    setTaskTitle("");
    setTasks((prev) => [t, ...prev]);
    if (human && bot) {
      await api.createHandoff(t.id, human.id, bot.id, "please take this");
      const listed = await api.listTasks(projectId);
      setTasks(listed.items);
    }
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
        </div>
        <form onSubmit={addMember} className="flex items-center gap-2 text-xs">
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
                  <span className="text-zinc-200">{m.body}</span>
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
            <h2 className="text-sm font-medium text-zinc-400">Tasks</h2>
            <form onSubmit={addTask} className="mt-2 flex gap-1">
              <input
                className="min-w-0 flex-1 rounded border border-zinc-800 bg-zinc-950 px-2 py-1 text-sm"
                value={taskTitle}
                onChange={(e) => setTaskTitle(e.target.value)}
                placeholder="New Task"
              />
              <button className="rounded bg-amber-400 px-2 text-sm text-zinc-950" type="submit">
                Add
              </button>
            </form>
            <ul className="mt-2 space-y-2">
              {tasks.map((t) => (
                <li key={t.id} className="rounded border border-zinc-800 px-2 py-1 text-sm">
                  <div className="flex items-center justify-between gap-2">
                    <span>{t.title}</span>
                    <select
                      className="bg-zinc-950 text-xs"
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
                  </div>
                </li>
              ))}
            </ul>
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
                    <p className="text-emerald-400">Answer: {d.answer}</p>
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
        </div>
      </div>
    </main>
  );
}

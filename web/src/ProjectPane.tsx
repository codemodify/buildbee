import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api } from "./api";
import { Empty, formatError, renderMentions } from "./ui";
import type {
  Channel,
  Decision,
  Invite,
  Member,
  Message,
  Project,
  Routine,
  Task,
} from "./types";

export function ProjectPane({
  projectId,
  channelId,
  focused = false,
  compact = false,
  showClose = false,
  onFocus,
  onClose,
  channelsVersion = 0,
}: {
  projectId: string;
  channelId?: string;
  focused?: boolean;
  compact?: boolean;
  showClose?: boolean;
  onFocus?: () => void;
  onClose?: () => void;
  channelsVersion?: number;
}) {
  const [project, setProject] = useState<Project | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [body, setBody] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const [prompt, setPrompt] = useState("");
  const [options, setOptions] = useState("yes, no");
  const [memberName, setMemberName] = useState("");
  const [memberKind, setMemberKind] = useState("human");
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [handoffRole, setHandoffRole] = useState("scout");
  const [activity, setActivity] = useState<
    { id: string; type: string; payload: Record<string, unknown>; created_at: string }[]
  >([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteGithub, setInviteGithub] = useState("");
  const [inviteRole, setInviteRole] = useState("member");
  const [assigneeId, setAssigneeId] = useState("");
  const [mineOnly, setMineOnly] = useState(false);
  const [toast, setToast] = useState("");
  const [error, setError] = useState("");

  const activeChannel = useMemo(() => {
    if (channelId) return channels.find((c) => c.id === channelId) ?? channels[0];
    return channels[0];
  }, [channels, channelId]);

  const human = members.find((m) => m.kind === "human");
  const bots = members.filter((m) => m.kind === "bot");
  const canInvite = human?.role === "owner" || human?.role === "admin";

  async function loadProject() {
    const p = await api.getProject(projectId);
    setProject(p);
    const m = await api.listMembers(projectId);
    const me = m.items.find((x) => x.kind === "human");
    const [c, t, d, rts, act, inv] = await Promise.all([
      api.listChannels(projectId),
      api.listTasks(projectId),
      api.listDecisions(
        projectId,
        mineOnly && me?.id ? { mine: true, memberId: me.id } : undefined,
      ),
      api.listRoutines(projectId),
      api.listActivity(projectId),
      api.listInvites(projectId).catch(() => ({ items: [] as Invite[] })),
    ]);
    setActivity(act.items.slice(0, 12));
    setMembers(m.items);
    setChannels(c.items);
    setTasks(t.items);
    setDecisions(d.items);
    setRoutines(rts.items);
    setInvites(inv.items);
    return c.items;
  }

  useEffect(() => {
    void loadProject().catch((e) => setError(formatError(e)));
  }, [projectId, mineOnly, channelsVersion]);

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
    try {
      await api.postMessage(activeChannel.id, body.trim(), human.id);
      setBody("");
      const data = await api.listMessages(activeChannel.id);
      setMessages(data.items);
      setError("");
    } catch (err) {
      setError(formatError(err));
    }
  }

  async function addTask(e: FormEvent) {
    e.preventDefault();
    if (!taskTitle.trim()) return;
    try {
      await api.createTask(projectId, taskTitle.trim(), handoffRole);
      setTaskTitle("");
      setTasks((await api.listTasks(projectId)).items);
      setError("");
    } catch (err) {
      setError(formatError(err));
    }
  }

  async function addMember(e: FormEvent) {
    e.preventDefault();
    if (!memberName.trim()) return;
    try {
      await api.addMember(projectId, {
        display_name: memberName.trim(),
        kind: memberKind,
      });
      setMemberName("");
      setMembers((await api.listMembers(projectId)).items);
      setError("");
    } catch (err) {
      setError(formatError(err));
    }
  }

  async function addDecision(e: FormEvent) {
    e.preventDefault();
    if (!prompt.trim()) return;
    const opts = options
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      await api.createDecision(projectId, {
        prompt: prompt.trim(),
        options: opts,
        recommendation: opts[0] ?? "",
        assignee_id: assigneeId || undefined,
      });
      setPrompt("");
      const d = await api.listDecisions(projectId);
      setDecisions(d.items);
      setError("");
    } catch (err) {
      setError(formatError(err));
    }
  }

  const gridClass = compact
    ? "grid gap-2 p-2"
    : "grid min-h-[calc(100vh-4rem)] gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_18rem]";

  return (
    <main
      data-testid="project-pane"
      data-project-id={projectId}
      data-focused={focused ? "true" : "false"}
      className={`h-full overflow-auto bg-bb-bg text-bb-fg ${
        focused ? "ring-1 ring-inset ring-amber-400/80" : "ring-1 ring-inset ring-transparent"
      }`}
      onClick={onFocus}
    >
      <header className="border-b border-bb-border px-3 py-2">
        <div className="flex items-center justify-between gap-2">
          <h1 className="min-w-0 flex-1 truncate text-lg font-semibold">
            {project?.name ?? "Project"}
          </h1>
          {showClose ? (
            <button
              type="button"
              data-testid="pane-close"
              className="shrink-0 rounded bg-bb-inset px-2 py-1 text-bb-fg hover:text-bb-fg"
              aria-label="Close pane"
              onClick={(e) => {
                e.stopPropagation();
                onClose?.();
              }}
            >
              ×
            </button>
          ) : null}
        </div>
        <form onSubmit={addMember} className="mt-2 flex flex-wrap items-center gap-2 text-xs">
          <label className="flex items-center gap-1 text-bb-muted">
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
          <span className="text-bb-subtle">{members.length} Members</span>
          <input
            className="w-28 rounded border border-bb-border bg-bb-surface px-2 py-1"
            value={memberName}
            onChange={(e) => setMemberName(e.target.value)}
            placeholder="name"
          />
          <select
            className="bg-bb-surface"
            value={memberKind}
            onChange={(e) => setMemberKind(e.target.value)}
          >
            <option value="human">human</option>
            <option value="bot">bot</option>
          </select>
          <button type="submit" className="rounded bg-bb-inset px-2 py-1">
            Add Member
          </button>
        </form>
      </header>
      {error ? <p className="px-3 py-2 text-sm text-bb-danger">{error}</p> : null}
      {toast ? <p className="px-3 py-2 text-sm text-bb-success">{toast}</p> : null}
      <div className={gridClass}>
        <section className="flex min-h-[16rem] flex-col rounded-lg border border-bb-border bg-bb-surface">
          <div className="border-b border-bb-border px-3 py-2 text-sm text-bb-muted">
            #{activeChannel?.name ?? "…"}
          </div>
          <ul className="flex-1 space-y-2 overflow-auto p-3">
            {messages.length === 0 ? (
              <li>
                <Empty>No messages yet. Say hello or @mention a Bot.</Empty>
              </li>
            ) : null}
            {messages.map((m) => {
              const who =
                members.find((mem) => mem.id === m.member_id)?.display_name ?? m.member_id;
              return (
                <li key={m.id} className="text-sm">
                  <span className="font-medium text-bb-accent">{who}</span>{" "}
                  <span className="text-bb-fg">{renderMentions(m.body)}</span>
                </li>
              );
            })}
          </ul>
          <form onSubmit={send} className="flex gap-2 border-t border-bb-border p-3">
            <input
              className="flex-1 rounded-md border border-bb-border bg-bb-surface px-3 py-2"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder="Message the Channel"
              onFocus={onFocus}
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
          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="flex items-center justify-between text-sm font-medium text-bb-muted">
              Tasks
              <button
                type="button"
                className="rounded bg-bb-inset px-2 py-0.5 text-xs text-bb-fg"
                onClick={() => {
                  void api
                    .syncIssues(projectId)
                    .then(async () => {
                      setTasks((await api.listTasks(projectId)).items);
                      setToast("Issues synced");
                      setError("");
                    })
                    .catch((err) => setError(formatError(err)));
                }}
              >
                Sync Issues
              </button>
            </h2>
            <form onSubmit={addTask} className="mt-2 flex flex-wrap gap-1">
              <input
                className="min-w-0 flex-1 rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
                value={taskTitle}
                onChange={(e) => setTaskTitle(e.target.value)}
                placeholder="New Task"
              />
              <select
                className="rounded border border-bb-border bg-bb-surface px-1 text-xs"
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
                <div key={col} className="rounded border border-bb-border bg-bb-inset p-1">
                  <p className="px-1 text-[10px] uppercase tracking-wide text-bb-subtle">{col}</p>
                  <ul className="mt-1 space-y-1">
                    {tasks.filter((t) => t.status === col).length === 0 ? (
                      <li className="px-1 text-[10px] text-bb-subtle">None</li>
                    ) : null}
                    {tasks
                      .filter((t) => t.status === col)
                      .map((t) => (
                        <li key={t.id} className="rounded bg-bb-surface px-1.5 py-1 text-xs">
                          <a
                            href={`#/projects/${projectId}/tasks/${t.id}`}
                            className="block hover:text-bb-accent"
                          >
                            {t.title}
                          </a>
                          {t.issue_url ? (
                            <a className="text-[10px] text-amber-500 underline" href={t.issue_url}>
                              #{t.issue_number}
                            </a>
                          ) : null}
                          <select
                            className="mt-1 w-full bg-bb-surface text-[10px]"
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

          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="flex items-center justify-between text-sm font-medium text-bb-muted">
              Decisions
              <label className="flex items-center gap-1 text-xs font-normal">
                <input
                  type="checkbox"
                  checked={mineOnly}
                  onChange={(e) => setMineOnly(e.target.checked)}
                />
                mine
              </label>
            </h2>
            <form onSubmit={addDecision} className="mt-2 space-y-1">
              <input
                className="w-full rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="Decision prompt"
              />
              <input
                className="w-full rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
                value={options}
                onChange={(e) => setOptions(e.target.value)}
                placeholder="options, comma separated"
              />
              <select
                className="w-full rounded border border-bb-border bg-bb-surface px-2 py-1 text-xs"
                value={assigneeId}
                onChange={(e) => setAssigneeId(e.target.value)}
                aria-label="Decision assignee"
              >
                <option value="">All humans (no assignee)</option>
                {members
                  .filter((m) => m.kind === "human")
                  .map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.display_name}
                    </option>
                  ))}
              </select>
              <button className="rounded bg-bb-inset px-2 py-1 text-sm" type="submit">
                Ask
              </button>
            </form>
            <ul className="mt-3 space-y-3">
              {decisions.length === 0 ? (
                <li>
                  <Empty>No Decisions yet. Ask when a Bot needs a choice.</Empty>
                </li>
              ) : null}
              {decisions.map((d) => (
                <li key={d.id} className="text-sm">
                  <p className="font-medium">
                    {d.prompt}
                    {d.assignee_id ? (
                      <span className="ml-1 text-xs text-bb-subtle">
                        → {members.find((m) => m.id === d.assignee_id)?.display_name ?? "assigned"}
                      </span>
                    ) : null}
                  </p>
                  {d.answer ? (
                    <p className="text-bb-success">
                      Answer: {d.answer}
                      {d.reused ? (
                        <span className="ml-1 text-xs text-bb-subtle">(reused memory)</span>
                      ) : null}
                    </p>
                  ) : (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {(d.options.length ? d.options : ["yes"]).map((opt) => (
                        <button
                          key={opt}
                          type="button"
                          className="rounded bg-bb-inset px-2 py-0.5"
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

          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="text-sm font-medium text-bb-muted">Invites</h2>
            {canInvite ? (
              <form
                className="mt-2 space-y-1"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (!inviteEmail.trim() && !inviteGithub.trim()) {
                    setError("Email or GitHub login is required");
                    return;
                  }
                  void api
                    .createInvite(projectId, {
                      email: inviteEmail.trim() || undefined,
                      github_login: inviteGithub.trim() || undefined,
                      role: inviteRole,
                    })
                    .then(async () => {
                      setInviteEmail("");
                      setInviteGithub("");
                      setInvites((await api.listInvites(projectId)).items);
                      setError("");
                      setToast("Invite created");
                    })
                    .catch((err) => setError(formatError(err)));
                }}
              >
                <input
                  className="w-full rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder="email"
                />
                <input
                  className="w-full rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
                  value={inviteGithub}
                  onChange={(e) => setInviteGithub(e.target.value)}
                  placeholder="GitHub login"
                />
                <div className="flex gap-1">
                  <select
                    className="rounded border border-bb-border bg-bb-surface px-1 text-xs"
                    value={inviteRole}
                    onChange={(e) => setInviteRole(e.target.value)}
                    aria-label="Invite Role"
                  >
                    <option value="member">member</option>
                    <option value="admin">admin</option>
                  </select>
                  <button className="rounded bg-amber-400 px-2 text-sm text-zinc-950" type="submit">
                    Invite
                  </button>
                </div>
              </form>
            ) : (
              <Empty>Only owner or admin can send Invites.</Empty>
            )}
            <ul className="mt-3 space-y-2">
              {invites.length === 0 ? (
                <li>
                  <Empty>No pending Invites.</Empty>
                </li>
              ) : null}
              {invites.map((inv) => (
                <li key={inv.id} className="text-xs">
                  <p className="font-medium text-bb-fg">
                    {inv.email || inv.github_login}{" "}
                    <span className="uppercase text-bb-subtle">{inv.role}</span>
                  </p>
                  <div className="mt-1 flex flex-wrap gap-1">
                    <button
                      type="button"
                      className="rounded bg-bb-inset px-2 py-0.5"
                      onClick={() => {
                        const link = `${window.location.origin}/${inv.path ?? `#/invite/${inv.token}`}`;
                        void navigator.clipboard.writeText(link).then(
                          () => setToast("Invite link copied"),
                          () => setToast(link),
                        );
                      }}
                    >
                      Copy link
                    </button>
                    {canInvite ? (
                      <button
                        type="button"
                        className="rounded bg-bb-inset px-2 py-0.5"
                        onClick={() => {
                          void api.revokeInvite(inv.id).then(async () => {
                            setInvites((await api.listInvites(projectId)).items);
                          });
                        }}
                      >
                        Revoke
                      </button>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          </section>

          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="text-sm font-medium text-bb-muted">Bots + Roles</h2>
            <ul className="mt-2 space-y-2">
              {bots.map((b) => (
                <li key={b.id} className="text-sm">
                  <span className="font-medium text-bb-accent">{b.display_name}</span>{" "}
                  <span className="rounded bg-bb-inset px-1.5 py-0.5 text-xs uppercase text-bb-fg">
                    {b.role}
                  </span>
                  {b.instructions ? (
                    <p className="mt-0.5 text-xs text-bb-subtle">{b.instructions}</p>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>

          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="text-sm font-medium text-bb-muted">Activity</h2>
            <ul className="mt-2 max-h-40 space-y-1 overflow-auto text-xs text-bb-muted">
              {activity.map((a) => (
                <li key={a.id}>
                  <span className="text-bb-accent">{a.type}</span>{" "}
                  {a.payload?.action ? String(a.payload.action) : ""}{" "}
                  {a.payload?.title ? String(a.payload.title) : ""}
                  {a.payload?.name ? String(a.payload.name) : ""}
                </li>
              ))}
              {activity.length === 0 ? (
                <li>
                  <Empty>No Activity yet.</Empty>
                </li>
              ) : null}
            </ul>
          </section>

          <section className="rounded-lg border border-bb-border bg-bb-surface p-3">
            <h2 className="text-sm font-medium text-bb-muted">Routines</h2>
            <ul className="mt-2 space-y-2">
              {routines.length === 0 ? (
                <li>
                  <Empty>No Routines on this Project.</Empty>
                </li>
              ) : null}
              {routines.map((rt) => (
                <li key={rt.id} className="flex items-center justify-between text-sm">
                  <span>
                    {rt.name} <span className="text-bb-subtle">{rt.schedule}</span>
                  </span>
                  <button
                    type="button"
                    className="rounded bg-bb-inset px-2 py-0.5 text-xs"
                    onClick={() => {
                      void api
                        .fireRoutine(rt.id)
                        .then(async () => {
                          setTasks((await api.listTasks(projectId)).items);
                          setToast("Routine started");
                          setError("");
                        })
                        .catch((err) => setError(formatError(err)));
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

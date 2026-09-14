import { useEffect, useState, type FormEvent } from "react";
import {
  api,
  getServerOrigin,
  hydrateServerOrigin,
  isDesktopShell,
  persistServerOrigin,
  pingServer,
  runEventsWsUrl,
} from "./api";
import { parseHash, type Route } from "./hash";
import type {
  AuthMe,
  Member,
  Invite,
  Notification,
  Preferences,
  TaskDetail,
  RunEvent,
} from "./types";
import { Empty, formatError } from "./ui";
import { Workspace } from "./Workspace";

export default function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const onHash = () => setRoute(parseHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  useEffect(() => {
    void hydrateServerOrigin().finally(() => setReady(true));
  }, []);

  if (!ready) {
    return (
      <main className="min-h-screen bg-zinc-950 px-6 py-16 text-sm text-zinc-400">
        Connecting to Server…
      </main>
    );
  }

  if (route.page === "settings") {
    return (
      <>
        <Chrome />
        <SettingsPage />
      </>
    );
  }
  if (route.page === "invite") {
    return (
      <>
        <Chrome />
        <InvitePage token={route.token} />
      </>
    );
  }
  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-zinc-950">
      <Chrome />
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {route.page === "task" ? (
          <Workspace route={route}>
            <TaskPage projectId={route.projectId} taskId={route.taskId} />
          </Workspace>
        ) : (
          <Workspace route={route} />
        )}
      </div>
    </div>
  );
}

function Chrome() {
  const [me, setMe] = useState<AuthMe | null>(null);
  useEffect(() => {
    void api.authMe().then(setMe).catch(() => setMe(null));
  }, []);
  return (
    <div className="flex shrink-0 items-center justify-between gap-3 border-b border-zinc-800 bg-zinc-900 px-4 py-2 text-sm">
      <div className="min-w-0 flex-1 text-zinc-300">
        {!me ? (
          <span className="text-zinc-500">Connecting…</span>
        ) : me.dev ? (
          <span className="text-amber-200">
            Dev auth: acting as Member {me.identity?.display_name ?? "You"}
          </span>
        ) : me.signed_in ? (
          <span>Signed in as {me.identity?.github_login ?? me.identity?.display_name}</span>
        ) : (
          <a
            href={`${getServerOrigin()}/v1/auth/github`}
            className="rounded bg-zinc-100 px-3 py-1 font-medium text-zinc-950"
          >
            Sign in with GitHub
          </a>
        )}
      </div>
      {isDesktopShell() ? (
        <span className="hidden text-xs text-amber-400 sm:inline">Desktop</span>
      ) : null}
      <a href="#/settings" className="text-xs text-zinc-400 hover:text-zinc-200">
        Settings
      </a>
      <PrefsBar />
      <InboxBell />
    </div>
  );
}

function PrefsBar() {
  const [memberId, setMemberId] = useState("");
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    void (async () => {
      try {
        const projs = await api.listProjects();
        for (const p of projs.items) {
          const m = await api.listMembers(p.id);
          const human = m.items.find((x) => x.kind === "human");
          if (human) {
            setMemberId(human.id);
            setPrefs(await api.getPreferences(human.id));
            return;
          }
        }
      } catch (e) {
        setError(formatError(e));
      }
    })();
  }, []);

  if (!memberId || !prefs) {
    return error ? <span className="text-xs text-red-400">{error}</span> : null;
  }

  async function patch(next: Partial<Preferences>) {
    try {
      setPrefs(await api.patchPreferences(memberId, next));
    } catch (e) {
      setError(formatError(e));
    }
  }

  return (
    <div className="flex items-center gap-2 text-xs text-zinc-400">
      <label className="flex items-center gap-1">
        <input
          type="checkbox"
          checked={prefs.mute_mentions}
          onChange={(e) => void patch({ mute_mentions: e.target.checked })}
        />
        mute mentions
      </label>
      <label className="flex items-center gap-1">
        <input
          type="checkbox"
          checked={prefs.mute_routines}
          onChange={(e) => void patch({ mute_routines: e.target.checked })}
        />
        mute routines
      </label>
    </div>
  );
}

function InboxBell() {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [error, setError] = useState("");

  async function load() {
    const projs = await api.listProjects();
    const ids: string[] = [];
    const seen = new Set<string>();
    for (const p of projs.items) {
      const members = await api.listMembers(p.id);
      for (const mem of members.items) {
        if (mem.kind === "human" && !seen.has(mem.id)) {
          seen.add(mem.id);
          ids.push(mem.id);
        }
      }
    }
    const list: Notification[] = [];
    let unreadN = 0;
    for (const id of ids) {
      const data = await api.listNotifications(id);
      list.push(...data.items);
      unreadN += data.unread;
    }
    list.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
    setMemberIds(ids);
    setItems(list);
    setUnread(unreadN);
    setError("");
  }

  useEffect(() => {
    let stop = false;
    async function tick() {
      try {
        if (!stop) await load();
      } catch (e) {
        if (!stop) setError(formatError(e));
      }
    }
    void tick();
    const id = window.setInterval(() => void tick(), 8000);
    return () => {
      stop = true;
      window.clearInterval(id);
    };
  }, []);

  async function markRead(n: Notification) {
    await api.readNotification(n.id);
    if (n.href) {
      window.location.hash = n.href.startsWith("#") ? n.href : `#${n.href}`;
    }
    await load();
  }

  async function markAll() {
    for (const id of memberIds) {
      await api.readAllNotifications(id);
    }
    await load();
  }

  return (
    <div className="relative">
      <button
        type="button"
        className="relative rounded bg-zinc-800 px-2 py-1 text-zinc-100"
        onClick={() => setOpen((v) => !v)}
        aria-label="Notifications inbox"
      >
        Inbox
        {unread > 0 ? (
          <span className="ml-1 rounded-full bg-amber-400 px-1.5 text-xs font-medium text-zinc-950">
            {unread}
          </span>
        ) : null}
      </button>
      {open ? (
        <div className="absolute right-0 z-20 mt-2 w-80 rounded-md border border-zinc-700 bg-zinc-950 p-2 shadow-lg">
          <div className="mb-2 flex items-center justify-between">
            <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Notifications
            </p>
            <button
              type="button"
              className="text-xs text-amber-300"
              onClick={() => void markAll()}
            >
              Mark all read
            </button>
          </div>
          {error ? <p className="mb-2 text-xs text-red-400">{error}</p> : null}
          <ul className="max-h-72 space-y-1 overflow-auto">
            {items.map((n) => (
              <li key={n.id}>
                <button
                  type="button"
                  className={`w-full rounded px-2 py-1.5 text-left text-sm ${
                    n.read_at ? "text-zinc-500" : "bg-zinc-900 text-zinc-100"
                  }`}
                  onClick={() => void markRead(n)}
                >
                  <span className="text-[10px] uppercase text-amber-400">{n.kind}</span>
                  <span className="mt-0.5 block">{n.title}</span>
                </button>
              </li>
            ))}
          </ul>
          {items.length === 0 ? <Empty>You&apos;re all caught up.</Empty> : null}
        </div>
      ) : null}
    </div>
  );
}

function SettingsPage() {
  const [url, setUrl] = useState(getServerOrigin() || "http://127.0.0.1:8080");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSave(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const saved = await persistServerOrigin(url);
      setUrl(saved);
      const ok = await pingServer(saved);
      setStatus(
        ok
          ? `Saved. Server at ${saved || window.location.origin} is reachable.`
          : `Saved ${saved || "(same origin)"}. /healthz did not return ok — is the Server running?`,
      );
    } catch (err) {
      setError(formatError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-16 text-zinc-100">
      <div className="mx-auto max-w-xl">
        <a href="#/" className="text-sm text-amber-400">
          BuildBee
        </a>
        <h1 className="mt-2 text-3xl font-semibold">Settings</h1>
        <p className="mt-3 text-sm text-zinc-400">
          The desktop shell loads this web UI and talks to a local or remote{" "}
          <span className="text-zinc-200">Server</span>. Hash routes such as{" "}
          <code className="text-zinc-300">#/invite/…</code> keep working.
          The Server (and Postgres, if you use it) run separately — they are
          not bundled in this app.
        </p>
        <form onSubmit={onSave} className="mt-8 space-y-3">
          <label className="block text-sm text-zinc-300">
            Server URL
            <input
              className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-900 px-3 py-2"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="http://127.0.0.1:8080"
              autoComplete="off"
            />
          </label>
          <p className="text-xs text-zinc-500">
            Default is <code>http://127.0.0.1:8080</code> in the desktop app.
            In a browser the UI stays same-origin unless you set a URL here.
            Settings persist in local storage
            {isDesktopShell() ? " and the desktop config directory" : ""}.
          </p>
          <button
            type="submit"
            disabled={busy}
            className="rounded-md bg-amber-400 px-4 py-2 font-medium text-zinc-950 disabled:opacity-50"
          >
            {busy ? "Saving…" : "Save and ping Server"}
          </button>
        </form>
        {status ? <p className="mt-3 text-sm text-emerald-300">{status}</p> : null}
        {error ? <p className="mt-3 text-sm text-red-400">{error}</p> : null}
      </div>
    </main>
  );
}

function eventText(ev: RunEvent): string {
  const p = ev.payload ?? {};
  if (typeof p.text === "string") return p.text;
  if (typeof p.summary === "string") return p.summary;
  if (typeof p.detail === "string") return p.detail;
  if (typeof p.status === "string") return p.status;
  return "";
}

function RunTranscript({ runId, status }: { runId: string; status: string }) {
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [live, setLive] = useState<"ws" | "poll" | "off">("off");

  useEffect(() => {
    let stop = false;
    let after = 0;
    let ws: WebSocket | null = null;
    let pollId = 0;

    function merge(items: RunEvent[]) {
      if (!items.length) return;
      setEvents((prev) => {
        const seen = new Set(prev.map((e) => e.id));
        const next = [...prev];
        for (const ev of items) {
          if (!seen.has(ev.id)) {
            next.push(ev);
            seen.add(ev.id);
          }
        }
        next.sort((a, b) => a.seq - b.seq);
        after = next.reduce((m, e) => Math.max(m, e.seq), after);
        return next;
      });
    }

    async function pull() {
      const data = await api.listRunEvents(runId, after);
      merge(data.items);
    }

    function startPoll() {
      setLive("poll");
      void pull().catch(() => undefined);
      pollId = window.setInterval(() => {
        void pull().catch(() => undefined);
      }, 800);
    }

    void pull()
      .then(() => {
        if (stop) return;
        try {
          ws = new WebSocket(runEventsWsUrl(runId));
          ws.onopen = () => setLive("ws");
          ws.onmessage = (msg) => {
            try {
              merge([JSON.parse(msg.data as string) as RunEvent]);
            } catch {
              /* ignore */
            }
          };
          ws.onerror = () => {
            if (!stop && !pollId) startPoll();
          };
          ws.onclose = () => {
            if (!stop && !pollId) startPoll();
          };
        } catch {
          startPoll();
        }
      })
      .catch(() => {
        if (!stop) startPoll();
      });

    return () => {
      stop = true;
      if (ws) ws.close();
      if (pollId) window.clearInterval(pollId);
    };
  }, [runId]);

  const tokens = events.filter((e) => e.kind === "token").map(eventText).join("");
  const tools = events.filter((e) => e.kind === "tool_call" || e.kind === "tool_result");
  const logs = events.filter((e) => e.kind === "log");

  if (events.length === 0) {
    return (
      <p className="mt-2 text-xs text-zinc-500">
        Transcript idle ({status}
        {live === "poll" ? " · polling" : live === "ws" ? " · live" : ""}).
      </p>
    );
  }

  return (
    <div className="mt-3 space-y-2">
      <p className="text-[10px] uppercase tracking-wide text-zinc-500">
        Live transcript
        {live === "ws" ? " · websocket" : live === "poll" ? " · poll" : ""}
      </p>
      {tokens ? (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-zinc-950 px-2 py-1 text-xs text-zinc-200">
          {tokens}
        </pre>
      ) : null}
      {tools.map((ev) => (
        <div
          key={ev.id}
          className={`rounded px-2 py-1 text-xs ${
            ev.kind === "tool_call"
              ? "border border-amber-700/60 bg-amber-950/40 text-amber-100"
              : "border border-zinc-700 bg-zinc-900 text-zinc-300"
          }`}
        >
          <span className="font-medium uppercase">{ev.kind.replace("_", " ")}</span>{" "}
          {String(ev.payload?.name ?? "")}
          {eventText(ev) ? <span className="block text-zinc-400">{eventText(ev)}</span> : null}
        </div>
      ))}
      {logs.map((ev) => (
        <pre key={ev.id} className="text-[11px] text-zinc-500">
          {eventText(ev)}
        </pre>
      ))}
    </div>
  );
}

function TaskPage({ projectId, taskId }: { projectId: string; taskId: string }) {
  const [detail, setDetail] = useState<TaskDetail | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [toRole, setToRole] = useState("builder");
  const [note, setNote] = useState("");
  const [autorun, setAutorun] = useState(false);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");

  async function refresh() {
    const [d, m] = await Promise.all([
      api.getTaskDetail(taskId),
      api.listMembers(projectId),
    ]);
    setDetail(d);
    setMembers(m.items);
  }

  useEffect(() => {
    void refresh().catch((e) => setError(formatError(e)));
  }, [taskId, projectId]);

  useEffect(() => {
    const active = (detail?.runs ?? []).some((r) => r.status === "pending" || r.status === "running");
    if (!active) return;
    const id = window.setInterval(() => {
      void refresh().catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(id);
  }, [detail?.runs, taskId, projectId]);

  const human = members.find((m) => m.kind === "human");
  const bots = members.filter((m) => m.kind === "bot");

  async function handoff(e: FormEvent) {
    e.preventDefault();
    if (!human) return;
    try {
      await api.createHandoff(taskId, human.id, "", note.trim() || "please take this", {
        toRole,
        autorun,
      });
      setNote("");
      setError("");
      await refresh();
    } catch (err) {
      setError(formatError(err));
    }
  }

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-8 text-zinc-100">
      <a href={`#/projects/${projectId}`} className="text-sm text-amber-400">
        ← Project
      </a>
      <h1 className="mt-3 text-2xl font-semibold">{detail?.title ?? "Task"}</h1>
      <p className="text-sm text-zinc-500">status {detail?.status}</p>
      {error ? <p className="mt-2 text-sm text-red-400">{error}</p> : null}
      {toast ? <p className="mt-2 text-sm text-emerald-400">{toast}</p> : null}
      <button
        type="button"
        className="mt-3 rounded bg-zinc-800 px-3 py-1 text-sm"
        onClick={() => {
          void api
            .startRun(taskId)
            .then(async () => {
              setToast("Run started");
              setError("");
              await refresh();
            })
            .catch((err) => setError(formatError(err)));
        }}
      >
        Start Run
      </button>
      {detail?.issue_url ? (
        <p className="mt-1 text-sm">
          Issues{" "}
          <a className="text-amber-300 underline" href={detail.issue_url}>
            #{detail.issue_number} {detail.issue_url}
          </a>
        </p>
      ) : null}

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
              <RunTranscript runId={r.id} status={r.status} />
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
          {detail && detail.artifacts.length === 0 ? (
            <li className="text-sm text-zinc-500">No Artifacts yet</li>
          ) : null}
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
          {detail && detail.pipelines.length === 0 ? (
            <li className="text-sm text-zinc-500">No Pipelines yet</li>
          ) : null}
        </ul>
      </section>
    </main>
  );
}

function InvitePage({ token }: { token: string }) {
  const [invite, setInvite] = useState<Invite | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void api
      .getInvite(token)
      .then(setInvite)
      .catch((e) => setError(formatError(e)));
  }, [token]);

  async function accept(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await api.acceptInvite(token, {
        display_name: name.trim() || undefined,
        github_login: invite?.github_login || undefined,
      });
      const pid = res.invite.project_id || invite?.project_id;
      window.location.hash = pid ? `#/projects/${pid}` : "#/";
    } catch (err) {
      setError(formatError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="min-h-screen bg-zinc-950 px-6 py-16 text-zinc-100">
      <div className="mx-auto max-w-md rounded-lg border border-zinc-800 bg-zinc-900/40 p-6">
        <p className="text-sm text-amber-400">Project Invite</p>
        <h1 className="mt-2 text-2xl font-semibold">
          {invite?.project_name ?? "BuildBee"}
        </h1>
        {invite ? (
          <p className="mt-2 text-sm text-zinc-400">
            Role <span className="text-zinc-200">{invite.role}</span>
            {invite.email ? ` · ${invite.email}` : ""}
            {invite.github_login ? ` · @${invite.github_login}` : ""}
            {invite.status !== "pending" ? (
              <span className="block text-red-400">This Invite is {invite.status}.</span>
            ) : null}
          </p>
        ) : null}
        {error ? <p className="mt-3 text-sm text-red-400">{error}</p> : null}
        {invite?.status === "pending" ? (
          <form onSubmit={accept} className="mt-6 space-y-3">
            <input
              className="w-full rounded border border-zinc-800 bg-zinc-950 px-3 py-2"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Your display name (optional in dev)"
            />
            <button
              type="submit"
              disabled={busy}
              className="rounded-md bg-amber-400 px-4 py-2 font-medium text-zinc-950 disabled:opacity-50"
            >
              {busy ? "Joining…" : "Accept Invite"}
            </button>
          </form>
        ) : invite ? (
          <Empty>Ask the Project owner for a new Invite link.</Empty>
        ) : (
          <Empty>Loading Invite…</Empty>
        )}
      </div>
    </main>
  );
}

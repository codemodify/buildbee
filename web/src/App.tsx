import { useEffect, useState, type FormEvent } from "react";
import { api, runEventsWsUrl } from "./api";
import { parseHash, type Route } from "./hash";
import { MeContext, useMe } from "./me";
import type {
  Artifact,
  Member,
  Notification,
  Person,
  Preferences,
  TaskDetail,
  RunEvent,
} from "./types";
import { Empty, formatError } from "./ui";
import { Workspace } from "./Workspace";

export default function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  const [me, setMe] = useState<Person | null | undefined>(undefined);
  useEffect(() => {
    const onHash = () => setRoute(parseHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  useEffect(() => {
    void api.me().then((r) => setMe(r.person)).catch(() => setMe(null));
  }, []);

  if (me === undefined) {
    return <main className="min-h-screen bg-bb-bg px-6 py-16 text-sm text-bb-muted">Connecting…</main>;
  }
  if (me === null) {
    return <NamePrompt onDone={setMe} />;
  }
  return (
    <MeContext.Provider value={me}>
    <div className="flex h-dvh flex-col overflow-hidden bg-bb-bg">
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
    </MeContext.Provider>
  );
}

function NamePrompt({ onDone }: { onDone: (p: Person) => void }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      onDone((await api.setMe(name.trim())).person);
    } catch (err) {
      setError(formatError(err));
    }
  }
  return (
    <main className="flex min-h-screen items-center justify-center bg-bb-bg px-6 text-bb-fg">
      <form onSubmit={submit} className="w-full max-w-sm space-y-3">
        <h1 className="text-2xl font-semibold">Welcome to BuildBee</h1>
        <p className="text-sm text-bb-muted">
          What should people and Bots call you? There is no password: this Server trusts its network.
        </p>
        <input
          id="your-name"
          autoFocus
          className="w-full rounded-md border border-bb-border bg-bb-surface px-3 py-2"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Your name"
          maxLength={60}
        />
        {error ? <p className="text-sm text-bb-danger">{error}</p> : null}
        <button type="submit" disabled={!name.trim()} className="rounded-md bg-amber-400 px-4 py-2 font-medium text-zinc-950 disabled:opacity-50">
          Continue
        </button>
      </form>
    </main>
  );
}

function Chrome() {
  return (
    <div className="flex shrink-0 items-center justify-between gap-3 border-b border-bb-border bg-bb-surface px-4 py-2 text-sm">
      <div className="min-w-0 flex-1 font-medium text-bb-fg">BuildBee</div>
      <WhoAmI />
      <PrefsBar />
      <InboxBell />
    </div>
  );
}

function WhoAmI() {
  const me = useMe();
  return (
    <span className="text-xs text-bb-muted">
      {me?.name}{" "}
      <button
        type="button"
        className="text-bb-accent"
        onClick={() => void api.forgetMe().then(() => window.location.reload())}
      >
        switch
      </button>
    </span>
  );
}

function PrefsBar() {
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    void api.getPreferences().then(setPrefs).catch((e) => setError(formatError(e)));
  }, []);

  if (!prefs) {
    return error ? <span className="text-xs text-bb-danger">{error}</span> : null;
  }

  async function patch(next: Partial<Preferences>) {
    try {
      setPrefs(await api.patchPreferences(next));
    } catch (e) {
      setError(formatError(e));
    }
  }

  return (
    <div className="flex items-center gap-2 text-xs text-bb-muted">
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
  const [error, setError] = useState("");

  async function load() {
    const data = await api.listNotifications();
    setItems(data.items);
    setUnread(data.unread);
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
    await api.readAllNotifications();
    await load();
  }

  return (
    <div className="relative">
      <button
        type="button"
        className="relative rounded bg-bb-inset px-2 py-1 text-bb-fg"
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
        <div className="absolute right-0 z-20 mt-2 w-80 rounded-md border border-bb-border bg-bb-surface p-2 shadow-lg">
          <div className="mb-2 flex items-center justify-between">
            <p className="text-xs font-medium uppercase tracking-wide text-bb-subtle">
              Notifications
            </p>
            <button
              type="button"
              className="text-xs text-bb-accent"
              onClick={() => void markAll()}
            >
              Mark all read
            </button>
          </div>
          {error ? <p className="mb-2 text-xs text-bb-danger">{error}</p> : null}
          <ul className="max-h-72 space-y-1 overflow-auto">
            {items.map((n) => (
              <li key={n.id}>
                <button
                  type="button"
                  className={`w-full rounded px-2 py-1.5 text-left text-sm ${
                    n.read_at ? "text-bb-subtle" : "bg-bb-surface text-bb-fg"
                  }`}
                  onClick={() => void markRead(n)}
                >
                  <span className="text-[10px] uppercase text-bb-accent">{n.kind}</span>
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
          ws = new WebSocket(runEventsWsUrl(runId, after));
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
      <p className="mt-2 text-xs text-bb-subtle">
        Transcript idle ({status}
        {live === "poll" ? " · polling" : live === "ws" ? " · live" : ""}).
      </p>
    );
  }

  return (
    <div className="mt-3 space-y-2">
      <p className="text-[10px] uppercase tracking-wide text-bb-subtle">
        Live transcript
        {live === "ws" ? " · websocket" : live === "poll" ? " · poll" : ""}
      </p>
      {tokens ? (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-bb-inset px-2 py-1 text-xs text-bb-fg">
          {tokens}
        </pre>
      ) : null}
      {tools.map((ev) => (
        <div
          key={ev.id}
          className={`rounded px-2 py-1 text-xs ${
            ev.kind === "tool_call"
              ? "border border-amber-200 bg-amber-50 text-amber-900"
              : "border border-bb-border bg-bb-surface text-bb-fg"
          }`}
        >
          <span className="font-medium uppercase">{ev.kind.replace("_", " ")}</span>{" "}
          {String(ev.payload?.name ?? "")}
          {eventText(ev) ? <span className="block text-bb-muted">{eventText(ev)}</span> : null}
        </div>
      ))}
      {logs.map((ev) => (
        <pre key={ev.id} className="text-[11px] text-bb-subtle">
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

  const bots = members.filter((m) => m.kind === "bot");

  async function handoff(e: FormEvent) {
    e.preventDefault();
    try {
      await api.createHandoff(taskId, note.trim() || "please take this", { toRole, autorun });
      setNote("");
      setError("");
      await refresh();
    } catch (err) {
      setError(formatError(err));
    }
  }

  return (
    <main className="min-h-screen bg-bb-bg px-6 py-8 text-bb-fg">
      <a href={`#/projects/${projectId}`} className="text-sm text-bb-accent">
        ← Project
      </a>
      <h1 className="mt-3 text-2xl font-semibold">{detail?.title ?? "Task"}</h1>
      <p className="text-sm text-bb-subtle">status {detail?.status}</p>
      {detail?.body ? (
        <p className="mt-2 max-w-3xl whitespace-pre-wrap text-sm text-bb-fg">{detail.body}</p>
      ) : null}
      {error ? <p className="mt-2 text-sm text-bb-danger">{error}</p> : null}
      {toast ? <p className="mt-2 text-sm text-bb-success">{toast}</p> : null}
      <button
        type="button"
        className="mt-3 rounded bg-bb-inset px-3 py-1 text-sm"
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
          <a className="text-bb-accent underline" href={detail.issue_url}>
            #{detail.issue_number} {detail.issue_url}
          </a>
        </p>
      ) : null}

      <form onSubmit={handoff} className="mt-6 flex flex-wrap items-end gap-2 rounded-lg border border-bb-border bg-bb-surface p-3">
        <label className="text-xs text-bb-muted">
          Handoff to Role
          <select
            className="mt-1 block rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
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
          className="min-w-[12rem] flex-1 rounded border border-bb-border bg-bb-surface px-2 py-1 text-sm"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Handoff notes"
        />
        <label className="flex items-center gap-1 text-xs text-bb-muted">
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
        <h2 className="text-sm font-medium text-bb-muted">Runs</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.runs ?? []).map((r) => (
            <li key={r.id} className="rounded border border-bb-border px-3 py-2 text-sm">
              <span className="text-bb-accent">{r.status}</span> {r.detail}
              <RunTranscript runId={r.id} status={r.status} />
            </li>
          ))}
          {detail && detail.runs.length === 0 ? (
            <li className="text-sm text-bb-subtle">No Runs yet</li>
          ) : null}
        </ul>
      </section>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-bb-muted">Artifacts</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.artifacts ?? []).map((a) => (
            <ArtifactItem key={a.id} artifact={a} />
          ))}
          {detail && detail.artifacts.length === 0 ? (
            <li className="text-sm text-bb-subtle">No Artifacts yet</li>
          ) : null}
        </ul>
      </section>

      <section className="mt-8">
        <h2 className="text-sm font-medium text-bb-muted">Pipelines</h2>
        <ul className="mt-2 space-y-2">
          {(detail?.pipelines ?? []).map((p) => (
            <li key={p.id} className="rounded border border-bb-border px-3 py-2 text-sm">
              <span className="text-bb-accent">{p.status}</span> {p.name}
              {p.external_url ? (
                <>
                  {" "}
                  <a className="text-bb-accent underline" href={p.external_url}>
                    {p.external_url}
                  </a>
                </>
              ) : null}
            </li>
          ))}
          {detail && detail.pipelines.length === 0 ? (
            <li className="text-sm text-bb-subtle">No Pipelines yet</li>
          ) : null}
        </ul>
      </section>
    </main>
  );
}

// ArtifactItem shows an Artifact; its body is fetched only when opened.
function ArtifactItem({ artifact: a }: { artifact: Artifact }) {
  const [body, setBody] = useState<string | null>(null);
  const [error, setError] = useState("");
  async function toggle() {
    if (body !== null) {
      setBody(null);
      return;
    }
    try {
      setBody((await api.getArtifact(a.id)).body ?? "");
    } catch (err) {
      setError(formatError(err));
    }
  }
  return (
    <li className="rounded border border-bb-border px-3 py-2 text-sm">
      <span className="text-bb-muted">{a.kind}</span> {a.name}
      {a.url ? (
        <>
          {" "}
          <a className="text-bb-accent underline" href={a.url}>
            {a.url}
          </a>
        </>
      ) : null}
      {a.size > 0 ? (
        <button type="button" className="ml-2 text-xs text-bb-accent" onClick={() => void toggle()}>
          {body === null ? `Show (${formatBytes(a.size)})` : "Hide"}
        </button>
      ) : null}
      {error ? <p className="text-xs text-bb-danger">{error}</p> : null}
      {body !== null ? <pre className="mt-2 max-h-96 overflow-auto text-xs text-bb-muted">{body}</pre> : null}
    </li>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

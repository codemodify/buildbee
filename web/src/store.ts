import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "./api";
import { live, useLiveStatus, useTopic } from "./live";
import { onSignal } from "./signals";
import type {
  Channel,
  Decision,
  DM,
  Member,
  Message,
  Notification,
  Posted,
  Presence,
  Project,
  Run,
  RunEvent,
  Task,
  Thread,
  Unread,
} from "./types";

/** useLoad runs load whenever deps change and on reload(). */
export function useLoad<T>(load: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string>("");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let alive = true;
    load()
      .then((d) => {
        if (alive) {
          setData(d);
          setError("");
        }
      })
      .catch((e: unknown) => alive && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      alive = false;
    };
    // Callers list what load depends on.
  }, [...deps, tick]);
  const reload = useCallback(() => setTick((t) => t + 1), []);
  return { data, error, reload, setData };
}

/** useThrottled calls fn at most every ms, trailing. */
function useThrottled(fn: () => void, ms: number) {
  const ref = useRef(fn);
  ref.current = fn;
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return useCallback(() => {
    if (timer.current !== undefined) return;
    timer.current = window.setTimeout(() => {
      timer.current = undefined;
      ref.current();
    }, ms);
  }, [ms]);
}

export type ProjectData = {
  project: Project;
  members: Member[];
  channels: Channel[];
  dms: Channel[];
  tasks: Task[];
  decisions: Decision[];
};

/**
 * useProject loads what the sidebar and boards need and refreshes it
 * whenever the Project's Activity moves.
 */
export function useProject(projectId: string | undefined) {
  const { data, error, reload } = useLoad<ProjectData | null>(async () => {
    if (!projectId) return null;
    const [project, dms, tasks, decisions] = await Promise.all([
      api.project(projectId),
      api.dms(projectId),
      api.tasks(projectId),
      api.decisions(projectId, true),
    ]);
    return {
      project,
      members: project.members ?? [],
      channels: project.channels ?? [],
      dms: dms.items,
      tasks: tasks.items,
      decisions: decisions.items,
    };
  }, [projectId]);
  const refresh = useThrottled(reload, 400);
  useTopic(projectId ? `project:${projectId}` : null, null, refresh);
  return { data: data ?? undefined, error, reload };
}

/** useChannel holds a Channel's root messages, live. */
export function useChannel(channelId: string | undefined) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [cursor, setCursor] = useState<number | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    setMessages([]);
    setCursor(null);
    if (!channelId) return;
    let alive = true;
    api
      .messages(channelId)
      .then((page) => {
        if (!alive) return;
        setMessages(page.items);
        setHasMore(page.has_more);
        setCursor(page.items.length ? page.items[page.items.length - 1].seq : 0);
        setError("");
      })
      .catch((e: unknown) => alive && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      alive = false;
    };
  }, [channelId]);
  useTopic(channelId && cursor !== null ? `channel:${channelId}` : null, cursor, (f) => {
    if (f.type === "message_edited" || f.type === "message_deleted") {
      const m = f.data as Message;
      setMessages((ms) => ms.map((x) => (x.id === m.id ? { ...x, ...m } : x)));
      return;
    }
    if (f.type !== "message") return;
    const m = f.data as Posted & { root?: Message };
    if (m.thread_id) {
      const root = m.root;
      if (root) setMessages((ms) => ms.map((x) => (x.id === root.id ? { ...x, ...root } : x)));
      return;
    }
    setMessages((ms) => (ms.some((x) => x.id === m.id) ? ms.map((x) => (x.id === m.id ? { ...x, ...m } : x)) : [...ms, m]));
  });
  const loadOlder = useCallback(async () => {
    if (!channelId || !messages.length) return;
    const page = await api.messages(channelId, messages[0].seq);
    setMessages((ms) => [...page.items.filter((m) => !ms.some((x) => x.id === m.id)), ...ms]);
    setHasMore(page.has_more);
  }, [channelId, messages]);
  const add = useCallback((m: Message) => {
    setMessages((ms) => (ms.some((x) => x.id === m.id) ? ms : [...ms, m]));
  }, []);
  return { messages, hasMore, loadOlder, error, add };
}

/** useThread holds one thread, live. */
export function useThread(rootId: string | undefined) {
  const [thread, setThread] = useState<Thread | undefined>(undefined);
  const [error, setError] = useState("");
  useEffect(() => {
    setThread(undefined);
    if (!rootId) return;
    let alive = true;
    api
      .thread(rootId)
      .then((t) => alive && (setThread(t), setError("")))
      .catch((e: unknown) => alive && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      alive = false;
    };
  }, [rootId]);
  const cursor = thread ? Math.max(thread.root.seq, ...thread.replies.map((r) => r.seq)) : null;
  useTopic(thread ? `channel:${thread.root.channel_id}` : null, cursor, (f) => {
    if (f.type === "message_edited" || f.type === "message_deleted") {
      const m = f.data as Message;
      setThread((t) =>
        t && { ...t, root: t.root.id === m.id ? { ...t.root, ...m } : t.root, replies: t.replies.map((r) => (r.id === m.id ? { ...r, ...m } : r)) },
      );
      return;
    }
    if (f.type !== "message") return;
    const m = f.data as Posted & { root?: Message };
    setThread((t) => {
      if (!t) return t;
      if (m.id === t.root.id) return { ...t, root: { ...t.root, ...m } };
      if (m.thread_id !== t.root.id || t.replies.some((r) => r.id === m.id)) return t;
      return { ...t, root: m.root ? { ...t.root, ...m.root } : t.root, replies: [...t.replies, m] };
    });
  });
  const add = useCallback((m: Message) => {
    setThread((t) => (t && !t.replies.some((r) => r.id === m.id) ? { ...t, replies: [...t.replies, m] } : t));
  }, []);
  return { thread, error, add };
}

/**
 * useUnread counts unread messages per Channel and DM, live: messages from
 * others bump the count unless the Channel is open, which marks it read.
 */
export function useUnread(
  projectId: string | undefined,
  channelIds: string[],
  activeId: string | undefined,
  myMemberId: string | undefined,
) {
  const [counts, setCounts] = useState<Record<string, Unread>>({});
  useEffect(() => {
    setCounts({});
    if (!projectId) return;
    let alive = true;
    api.unread(projectId).then((r) => {
      if (!alive) return;
      const next: Record<string, Unread> = {};
      for (const u of r.items) next[u.channel_id] = u;
      setCounts(next);
    });
    return () => {
      alive = false;
    };
  }, [projectId]);
  // Opening a Channel reads it.
  const active = activeId ? counts[activeId] : undefined;
  useEffect(() => {
    if (!activeId || !active || active.unread === 0) return;
    void api.markRead(activeId, active.last_seq).catch(() => undefined);
    setCounts((c) => ({ ...c, [activeId]: { ...active, unread: 0 } }));
  }, [activeId, active]);
  const key = channelIds.join(",");
  useEffect(() => {
    const offs = channelIds.map((id) =>
      liveBump(id, (m) => {
        setCounts((c) => {
          const cur = c[id] ?? { channel_id: id, unread: 0, last_seq: 0 };
          const mine = myMemberId && m.member_id === myMemberId;
          return { ...c, [id]: { ...cur, last_seq: Math.max(cur.last_seq, m.seq), unread: cur.unread + (mine ? 0 : 1) } };
        });
      }),
    );
    return () => offs.forEach((off) => off());
  }, [key, myMemberId]);
  return counts;
}

// liveBump subscribes to a Channel's live messages (no replay).
function liveBump(channelId: string, fn: (m: Message) => void) {
  return live.subscribe(`channel:${channelId}`, null, (f) => {
    if (f.type === "message") fn(f.data as Message);
  });
}

/**
 * useDMs lists the Person's DMs across the Server, live: a new DM arrives
 * as a notification, a new message on a DM's own topic. Opening one reads it.
 */
export function useDMs(personId: string, activeId: string | undefined) {
  const { data, reload } = useLoad<DM[]>(() => api.directMessages().then((r) => r.items), [personId]);
  const refresh = useThrottled(reload, 300);
  useTopic(`person:${personId}`, null, refresh);
  useEffect(() => onSignal("dms", refresh), [refresh]);
  const dms = data ?? [];
  const key = dms.map((d) => d.id).join(",");
  useEffect(() => {
    const offs = dms.map((d) => liveBump(d.id, refresh));
    return () => offs.forEach((off) => off());
    // Resubscribe when the set of DMs changes, not on every count.
  }, [key]);
  const active = dms.find((d) => d.id === activeId);
  useEffect(() => {
    if (!active || active.unread === 0) return;
    void api.markRead(active.id, active.last_seq).then(refresh, () => undefined);
  }, [active?.id, active?.unread]);
  return { dms, reload };
}

/** useRun holds a Run and its events, live. */
export function useRun(runId: string | undefined) {
  const [run, setRun] = useState<Run | undefined>(undefined);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [cursor, setCursor] = useState<number | null>(null);
  useEffect(() => {
    setRun(undefined);
    setEvents([]);
    setCursor(null);
    if (!runId) return;
    let alive = true;
    Promise.all([api.run(runId), api.runEvents(runId)]).then(([r, evs]) => {
      if (!alive) return;
      setRun(r);
      setEvents(evs.items);
      setCursor(evs.items.length ? evs.items[evs.items.length - 1].seq : 0);
    });
    return () => {
      alive = false;
    };
  }, [runId]);
  useTopic(runId && cursor !== null ? `run:${runId}` : null, cursor, (f) => {
    const ev = f.data as RunEvent;
    setEvents((es) => (es.some((e) => e.seq === ev.seq) ? es : [...es, ev]));
    if (ev.kind === "status") {
      const status = String(ev.payload.status ?? "");
      if (status && runId) {
        setRun((r) => (r ? { ...r, status, detail: String(ev.payload.detail ?? r.detail) } : r));
        if (["succeeded", "failed", "canceled"].includes(status)) void api.run(runId).then(setRun);
      }
    }
  });
  return { run, events };
}

/** usePresence is who is online and which workers are polling. */
export function usePresence() {
  const { data, reload } = useLoad<Presence>(() => api.presence(), []);
  const refresh = useThrottled(reload, 500);
  useTopic("presence:server", null, refresh);
  // Our own arrival may be announced before we subscribe: look again
  // whenever the socket (re)connects.
  const status = useLiveStatus();
  useEffect(() => {
    if (status === "open") refresh();
  }, [status, refresh]);
  useEffect(() => {
    const t = window.setInterval(reload, 60_000); // workers age out
    return () => window.clearInterval(t);
  }, [reload]);
  return data;
}

/** useInbox is the acting Person's notifications, live. */
export function useInbox(personId: string | undefined) {
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const load = useCallback(() => {
    void api.notifications().then((r) => {
      setItems(r.items);
      setUnread(r.unread);
    });
  }, []);
  useEffect(load, [load, personId]);
  useTopic(personId ? `person:${personId}` : null, null, (f) => {
    const n = f.data as Notification;
    setItems((xs) => [n, ...xs.filter((x) => x.id !== n.id)]);
    if (!n.read_at) setUnread((u) => u + 1);
  });
  const markAll = useCallback(async () => {
    await api.readAllNotifications();
    load();
  }, [load]);
  const markOne = useCallback(
    async (id: string) => {
      await api.readNotification(id);
      load();
    },
    [load],
  );
  return { items, unread, markAll, markOne };
}

/** useMembers indexes a Project's Members by ID. */
export function useMemberIndex(members: Member[] | undefined) {
  return useMemo(() => {
    const byId = new Map<string, Member>();
    for (const m of members ?? []) byId.set(m.id, m);
    return byId;
  }, [members]);
}

const SERVER_URL_KEY = "buildbee.server_url";
const DEFAULT_DESKTOP_SERVER = "http://127.0.0.1:8080";

type TauriCore = {
  invoke: <T>(cmd: string, args?: Record<string, unknown>) => Promise<T>;
};

function tauriCore(): TauriCore | undefined {
  const w = window as unknown as { __TAURI__?: { core?: TauriCore } };
  return w.__TAURI__?.core;
}

export function isDesktopShell(): boolean {
  return (
    typeof window !== "undefined" &&
    ("__TAURI_INTERNALS__" in window || "__TAURI__" in window)
  );
}

function readStoredOrigin(): string | null {
  try {
    return localStorage.getItem(SERVER_URL_KEY);
  } catch {
    return null;
  }
}

function defaultOrigin(): string {
  return isDesktopShell() ? DEFAULT_DESKTOP_SERVER : "";
}

let serverOrigin = (readStoredOrigin() ?? defaultOrigin()).replace(/\/$/, "");

export function getServerOrigin(): string {
  return serverOrigin;
}

export function apiUrl(path: string): string {
  if (path.startsWith("http://") || path.startsWith("https://")) {
    return path;
  }
  return `${serverOrigin}${path}`;
}

export function setServerOrigin(url: string): string {
  serverOrigin = url.trim().replace(/\/$/, "");
  try {
    if (serverOrigin) {
      localStorage.setItem(SERVER_URL_KEY, serverOrigin);
    } else {
      localStorage.removeItem(SERVER_URL_KEY);
    }
  } catch {
    /* ignore quota / private mode */
  }
  return serverOrigin;
}

export async function hydrateServerOrigin(): Promise<string> {
  const core = tauriCore();
  if (core) {
    try {
      const settings = await core.invoke<{ server_url?: string }>("get_settings");
      if (settings?.server_url) {
        setServerOrigin(settings.server_url);
      }
    } catch {
      /* keep localStorage / default */
    }
  } else if (isDesktopShell() && !readStoredOrigin()) {
    setServerOrigin(DEFAULT_DESKTOP_SERVER);
  }
  return serverOrigin;
}

export async function persistServerOrigin(url: string): Promise<string> {
  const next = setServerOrigin(url);
  const core = tauriCore();
  if (core) {
    await core.invoke("set_server_url", { url: next || DEFAULT_DESKTOP_SERVER });
  }
  return getServerOrigin();
}

export async function pingServer(origin = getServerOrigin()): Promise<boolean> {
  const base = (origin || window.location.origin).replace(/\/$/, "");
  const res = await fetch(`${base}/healthz`, { credentials: "omit" });
  if (!res.ok) return false;
  const body = (await res.json()) as { status?: string };
  return body.status === "ok";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const url = apiUrl(path);
  const cross = Boolean(serverOrigin);
  const res = await fetch(url, {
    credentials: cross ? "omit" : "include",
    ...init,
    headers,
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(text || res.statusText);
  }
  return text ? (JSON.parse(text) as T) : ({} as T);
}

export const api = {
  listProjects: () =>
    request<{ items: { id: string; name: string }[] }>("/v1/projects"),
  createProject: (name: string) =>
    request<import("./types").Project>("/v1/projects", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  getProject: (id: string) =>
    request<import("./types").Project>(`/v1/projects/${id}`),
  updateProject: (id: string, body: { auto_run: boolean }) =>
    request<import("./types").Project>(`/v1/projects/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  listMembers: (projectId: string) =>
    request<{ items: import("./types").Member[] }>(
      `/v1/projects/${projectId}/members`,
    ),
  addMember: (
    projectId: string,
    body: { display_name: string; kind: string },
  ) =>
    request(`/v1/projects/${projectId}/members`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  listChannels: (projectId: string) =>
    request<{ items: import("./types").Channel[] }>(
      `/v1/projects/${projectId}/channels`,
    ),
  createChannel: (projectId: string, name: string) =>
    request<import("./types").Channel>(`/v1/projects/${projectId}/channels`, {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  listMessages: (channelId: string) =>
    request<{ items: import("./types").Message[] }>(
      `/v1/channels/${channelId}/messages`,
    ),
  postMessage: (channelId: string, body: string, memberId: string) =>
    request<import("./types").Message>(`/v1/channels/${channelId}/messages`, {
      method: "POST",
      body: JSON.stringify({ body, member_id: memberId }),
    }),
  listTasks: (projectId: string) =>
    request<{ items: import("./types").Task[] }>(
      `/v1/projects/${projectId}/tasks`,
    ),
  createTask: (projectId: string, title: string, handoffRole = "scout") =>
    request<import("./types").Task & { handoff?: { id: string } }>(
      `/v1/projects/${projectId}/tasks?handoff=${encodeURIComponent(handoffRole)}`,
      {
        method: "POST",
        body: JSON.stringify({ title, handoff_role: handoffRole }),
      },
    ),
  updateTask: (taskId: string, status: string) =>
    request(`/v1/tasks/${taskId}`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    }),
  createHandoff: (
    taskId: string,
    from: string,
    to: string,
    note: string,
    opts?: { toRole?: string; autorun?: boolean },
  ) => {
    const q = opts?.autorun ? "?autorun=1" : "";
    return request(`/v1/tasks/${taskId}/handoffs${q}`, {
      method: "POST",
      body: JSON.stringify({
        from_member_id: from,
        to_member_id: to,
        to_role: opts?.toRole ?? "",
        note,
      }),
    });
  },
  listDecisions: (projectId: string, opts?: { inbox?: boolean; mine?: boolean; memberId?: string }) => {
    const q = new URLSearchParams();
    if (opts?.inbox) q.set("inbox", "1");
    if (opts?.mine) q.set("mine", "1");
    if (opts?.memberId) q.set("member_id", opts.memberId);
    const suffix = q.toString() ? `?${q.toString()}` : "";
    return request<{ items: import("./types").Decision[] }>(
      `/v1/projects/${projectId}/decisions${suffix}`,
    );
  },
  listDecisionMemories: (projectId: string) =>
    request<{ items: { fingerprint: string; prompt: string; answer: string }[] }>(
      `/v1/projects/${projectId}/decisions/memories`,
    ),
  createDecision: (
    projectId: string,
    body: { prompt: string; options: string[]; recommendation: string; assignee_id?: string },
  ) =>
    request(`/v1/projects/${projectId}/decisions`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  answerDecision: (id: string, answer: string) =>
    request(`/v1/decisions/${id}/answer`, {
      method: "POST",
      body: JSON.stringify({ answer }),
    }),
  startRun: (taskId: string) =>
    request<import("./types").Run>(`/v1/tasks/${taskId}/runs`, {
      method: "POST",
      body: "{}",
    }),
  getTaskDetail: (taskId: string) =>
    request<import("./types").TaskDetail>(`/v1/tasks/${taskId}/detail`),
  authMe: () => request<import("./types").AuthMe>("/v1/auth/me"),
  syncIssues: (projectId: string) =>
    request<{ items: import("./types").Task[]; fake?: boolean }>(
      `/v1/projects/${projectId}/issues/sync`,
      { method: "POST", body: JSON.stringify({ fake: true }) },
    ),
  listRoutines: (projectId: string) =>
    request<{ items: import("./types").Routine[] }>(
      `/v1/projects/${projectId}/routines`,
    ),
  fireRoutine: (id: string) =>
    request(`/v1/routines/${id}/run`, { method: "POST", body: "{}" }),
  listActivity: (projectId: string, type = "") =>
    request<{ items: { id: string; type: string; payload: Record<string, unknown>; created_at: string }[] }>(
      `/v1/projects/${projectId}/activity${type ? `?type=${encodeURIComponent(type)}` : ""}`,
    ),
  listNotifications: (memberId: string, unread = false) => {
    const q = new URLSearchParams();
    if (memberId) q.set("member_id", memberId);
    if (unread) q.set("unread", "1");
    const suffix = q.toString() ? `?${q.toString()}` : "";
    return request<{ items: import("./types").Notification[]; unread: number }>(
      `/v1/notifications${suffix}`,
    );
  },
  readNotification: (id: string) =>
    request<import("./types").Notification>(`/v1/notifications/${id}/read`, {
      method: "POST",
      body: "{}",
    }),
  readAllNotifications: (memberId: string) =>
    request<{ read: number }>(
      `/v1/notifications/read-all?member_id=${encodeURIComponent(memberId)}`,
      { method: "POST", body: "{}" },
    ),
  listInvites: (projectId: string) =>
    request<{ items: import("./types").Invite[] }>(
      `/v1/projects/${projectId}/invites`,
    ),
  createInvite: (
    projectId: string,
    body: { email?: string; github_login?: string; role: string },
  ) =>
    request<import("./types").Invite>(`/v1/projects/${projectId}/invites`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  getInvite: (token: string) =>
    request<import("./types").Invite>(`/v1/invites/${token}`),
  acceptInvite: (token: string, body?: { display_name?: string; github_login?: string }) =>
    request<{ already_member: boolean; member: import("./types").Member; invite: import("./types").Invite }>(
      `/v1/invites/${token}/accept`,
      { method: "POST", body: JSON.stringify(body ?? {}) },
    ),
  revokeInvite: (id: string) =>
    request<import("./types").Invite>(`/v1/invites/${id}`, { method: "DELETE" }),
  getPreferences: (memberId: string) =>
    request<import("./types").Preferences>(
      `/v1/me/preferences?member_id=${encodeURIComponent(memberId)}`,
    ),
  patchPreferences: (memberId: string, body: { mute_mentions?: boolean; mute_routines?: boolean }) =>
    request<import("./types").Preferences>(
      `/v1/me/preferences?member_id=${encodeURIComponent(memberId)}`,
      { method: "PATCH", body: JSON.stringify(body) },
    ),
};

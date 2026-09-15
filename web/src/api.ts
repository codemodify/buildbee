export function runEventsWsUrl(runId: string): string {
  const u = new URL(`/v1/runs/${runId}/ws`, window.location.origin);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  return u.toString();
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, { ...init, headers });
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
  listRunEvents: (runId: string, after = 0) =>
    request<{ items: import("./types").RunEvent[] }>(
      `/v1/runs/${runId}/events${after > 0 ? `?after=${after}` : ""}`,
    ),
  getTaskDetail: (taskId: string) =>
    request<import("./types").TaskDetail>(`/v1/tasks/${taskId}/detail`),
  syncIssues: (projectId: string) =>
    request<{ items: import("./types").Task[]; fake?: boolean }>(
      `/v1/projects/${projectId}/issues/sync`,
      { method: "POST", body: "{}" },
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

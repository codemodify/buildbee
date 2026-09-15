import type {
  Activity,
  Artifact,
  Channel,
  Decision,
  Member,
  Message,
  Notification,
  Person,
  Preferences,
  Project,
  Routine,
  Run,
  RunEvent,
  Task,
  TaskDetail,
} from "./types";

export function runEventsWsUrl(runId: string, after = 0): string {
  const u = new URL(`/v1/runs/${runId}/ws`, window.location.origin);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  if (after > 0) u.searchParams.set("after", String(after));
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

const post = (body: unknown = {}) => ({ method: "POST", body: JSON.stringify(body) });
const patch = (body: unknown) => ({ method: "PATCH", body: JSON.stringify(body) });

type Items<T> = { items: T[] };
type Page<T> = { items: T[]; has_more: boolean };

export const api = {
  // who is using this browser
  me: () => request<{ person: Person | null }>("/v1/me"),
  setMe: (name: string) => request<{ person: Person }>("/v1/me", post({ name })),
  forgetMe: () => request<unknown>("/v1/me", { method: "DELETE" }),

  listProjects: () => request<Items<Project>>("/v1/projects"),
  createProject: (name: string) => request<Project>("/v1/projects", post({ name })),
  getProject: (id: string) => request<Project>(`/v1/projects/${id}`),
  updateProject: (id: string, body: { auto_run?: boolean; name?: string; archived?: boolean }) =>
    request<Project>(`/v1/projects/${id}`, patch(body)),
  listMembers: (projectId: string) => request<Items<Member>>(`/v1/projects/${projectId}/members`),
  addMember: (projectId: string, body: { display_name: string; kind: string; role?: string }) =>
    request<Member>(`/v1/projects/${projectId}/members`, post(body)),
  listChannels: (projectId: string) => request<Items<Channel>>(`/v1/projects/${projectId}/channels`),
  createChannel: (projectId: string, name: string) =>
    request<Channel>(`/v1/projects/${projectId}/channels`, post({ name })),

  listMessages: (channelId: string) => request<Page<Message>>(`/v1/channels/${channelId}/messages`),
  postMessage: (channelId: string, body: string) =>
    request<Message>(`/v1/channels/${channelId}/messages`, post({ body })),

  listTasks: (projectId: string) => request<Items<Task>>(`/v1/projects/${projectId}/tasks`),
  createTask: (projectId: string, title: string, handoffRole = "scout") =>
    request<Task & { handoff?: { id: string } }>(`/v1/projects/${projectId}/tasks`, post({ title, handoff_role: handoffRole })),
  updateTask: (taskId: string, status: string) => request<Task>(`/v1/tasks/${taskId}`, patch({ status })),
  createHandoff: (taskId: string, note: string, opts: { toRole?: string; toMemberId?: string; autorun?: boolean }) =>
    request(`/v1/tasks/${taskId}/handoffs`, post({
      to_role: opts.toRole ?? "",
      to_member_id: opts.toMemberId ?? "",
      note,
      autorun: Boolean(opts.autorun),
    })),

  listDecisions: (projectId: string, opts?: { open?: boolean; mine?: boolean }) => {
    const q = new URLSearchParams();
    if (opts?.open) q.set("open", "1");
    if (opts?.mine) q.set("mine", "1");
    const suffix = q.toString() ? `?${q.toString()}` : "";
    return request<Items<Decision>>(`/v1/projects/${projectId}/decisions${suffix}`);
  },
  listDecisionMemories: (projectId: string) =>
    request<Items<{ fingerprint: string; prompt: string; answer: string }>>(`/v1/projects/${projectId}/decisions/memories`),
  createDecision: (projectId: string, body: { prompt: string; options: string[]; recommendation: string; assignee_id?: string }) =>
    request<Decision>(`/v1/projects/${projectId}/decisions`, post(body)),
  answerDecision: (id: string, answer: string) => request<Decision>(`/v1/decisions/${id}/answer`, post({ answer })),

  startRun: (taskId: string) => request<Run>(`/v1/tasks/${taskId}/runs`, post()),
  listRunEvents: (runId: string, after = 0) =>
    request<Page<RunEvent>>(`/v1/runs/${runId}/events${after > 0 ? `?after=${after}` : ""}`),
  getTaskDetail: (taskId: string) => request<TaskDetail>(`/v1/tasks/${taskId}/detail`),
  getArtifact: (id: string) => request<Artifact>(`/v1/artifacts/${id}`),

  syncIssues: (projectId: string) =>
    request<{ items: Task[]; fake?: boolean }>(`/v1/projects/${projectId}/issues/sync`, post()),
  listRoutines: (projectId: string) => request<Items<Routine>>(`/v1/projects/${projectId}/routines`),
  fireRoutine: (id: string) => request<Routine>(`/v1/routines/${id}/run`, post()),
  listActivity: (projectId: string, type = "") =>
    request<Page<Activity>>(`/v1/projects/${projectId}/activity${type ? `?type=${encodeURIComponent(type)}` : ""}`),

  // the inbox and preferences of whoever is using this browser
  listNotifications: (unread = false) =>
    request<{ items: Notification[]; unread: number; has_more: boolean }>(`/v1/me/notifications${unread ? "?unread=1" : ""}`),
  readNotification: (id: string) => request<Notification>(`/v1/notifications/${id}/read`, post()),
  readAllNotifications: () => request<{ read: number }>("/v1/me/notifications/read-all", post()),
  getPreferences: () => request<Preferences>("/v1/me/preferences"),
  patchPreferences: (body: { mute_mentions?: boolean; mute_routines?: boolean }) =>
    request<Preferences>("/v1/me/preferences", patch(body)),
};

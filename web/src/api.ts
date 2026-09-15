import type {
  Activity,
  Artifact,
  Channel,
  Decision,
  DM,
  Member,
  Message,
  Notification,
  Person,
  Posted,
  Preferences,
  Presence,
  Project,
  Roster,
  Routine,
  Run,
  RunEvent,
  Task,
  TaskDetail,
  Thread,
  Unread,
  Usage,
} from "./types";

/** An error the Server answered with; message is its explanation. */
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const res = await fetch(path, { ...init, headers, credentials: "same-origin" });
  const text = await res.text();
  if (!res.ok) {
    let msg = text || res.statusText;
    try {
      msg = (JSON.parse(text) as { error?: string }).error || msg;
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg);
  }
  return text ? (JSON.parse(text) as T) : ({} as T);
}

const post = (body: unknown = {}) => ({ method: "POST", body: JSON.stringify(body) });
const patch = (body: unknown) => ({ method: "PATCH", body: JSON.stringify(body) });
const q = (params: Record<string, string | number | boolean | undefined>) => {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "" && v !== false) s.set(k, String(v));
  const out = s.toString();
  return out ? `?${out}` : "";
};

type Items<T> = { items: T[] };
type Page<T> = { items: T[]; has_more: boolean };

export type ProjectPatch = Partial<
  Pick<Project, "name" | "auto_run" | "merge_policy" | "max_runs" | "instructions" | "repo_url" | "default_branch">
> & { archived?: boolean };

export const api = {
  // who is using this browser
  me: () => request<{ person: Person | null }>("/v1/me"),
  setMe: (name: string) => request<{ person: Person }>("/v1/me", post({ name })),
  rename: (name: string) => request<{ person: Person }>("/v1/me", patch({ name })),
  forgetMe: () => request<unknown>("/v1/me", { method: "DELETE" }),

  projects: () => request<Items<Project>>("/v1/projects"),
  createProject: (name: string, agent = "") => request<Project>("/v1/projects", post({ name, agent })),
  project: (id: string) => request<Project>(`/v1/projects/${id}`),
  updateProject: (id: string, body: ProjectPatch) => request<Project>(`/v1/projects/${id}`, patch(body)),
  addMember: (
    projectId: string,
    body: { display_name: string; kind: string; role?: string; agent?: string; instructions?: string },
  ) => request<Member>(`/v1/projects/${projectId}/members`, post(body)),
  leave: (projectId: string) => request<unknown>(`/v1/projects/${projectId}/leave`, post()),
  updateMember: (id: string, body: { display_name?: string; instructions?: string; agent?: string }) =>
    request<Member>(`/v1/members/${id}`, patch(body)),

  channels: (projectId: string) => request<Items<Channel>>(`/v1/projects/${projectId}/channels`),
  createChannel: (projectId: string, name: string) =>
    request<Channel>(`/v1/projects/${projectId}/channels`, post({ name })),
  directMessages: () => request<Items<DM>>("/v1/dms"),
  openDirect: (body: { person_ids?: string[]; member_id?: string }) => request<Channel>("/v1/dms", post(body)),
  dms: (projectId: string) => request<Items<Channel>>(`/v1/projects/${projectId}/dms`),
  openDM: (projectId: string, memberIds: string[]) =>
    request<Channel>(`/v1/projects/${projectId}/dms`, post({ member_ids: memberIds })),
  unread: (projectId: string) => request<Items<Unread>>(`/v1/projects/${projectId}/unread`),
  markRead: (channelId: string, seq: number) => request<unknown>(`/v1/channels/${channelId}/read`, post({ seq })),

  messages: (channelId: string, before?: number) =>
    request<Page<Message>>(`/v1/channels/${channelId}/messages${q({ before, limit: 100 })}`),
  postMessage: (channelId: string, body: string) =>
    request<Posted>(`/v1/channels/${channelId}/messages`, post({ body })),
  thread: (messageId: string) => request<Thread>(`/v1/messages/${messageId}/thread${q({ limit: 500 })}`),
  reply: (messageId: string, body: string) => request<Posted>(`/v1/messages/${messageId}/replies`, post({ body })),

  tasks: (projectId: string) => request<Items<Task>>(`/v1/projects/${projectId}/tasks`),
  createTask: (
    projectId: string,
    body: { title: string; body?: string; handoff_role?: string; channel_id?: string; autorun?: boolean },
  ) =>
    request<Task & { run?: Run }>(`/v1/projects/${projectId}/tasks`, post(body)),
  updateTask: (taskId: string, body: { title?: string; body?: string; status?: string }) =>
    request<Task>(`/v1/tasks/${taskId}`, patch(body)),
  taskDetail: (taskId: string) => request<TaskDetail>(`/v1/tasks/${taskId}/detail`),
  handOff: (taskId: string, body: { to_role?: string; to_member_id?: string; note?: string; autorun?: boolean }) =>
    request<unknown>(`/v1/tasks/${taskId}/handoffs`, post(body)),

  decisions: (projectId: string, open = false) =>
    request<Items<Decision>>(`/v1/projects/${projectId}/decisions${q({ open })}`),
  answer: (id: string, answer: string) => request<Decision>(`/v1/decisions/${id}/answer`, post({ answer })),

  startRun: (taskId: string, body: { agent?: string; bot_member_id?: string; kind?: string } = {}) =>
    request<Run>(`/v1/tasks/${taskId}/runs`, post(body)),
  run: (runId: string) => request<Run>(`/v1/runs/${runId}`),
  runEvents: (runId: string, after = 0) =>
    request<Page<RunEvent>>(`/v1/runs/${runId}/events${q({ after, limit: 1000 })}`),
  cancelRun: (runId: string) => request<Run>(`/v1/runs/${runId}`, patch({ status: "canceled", detail: "canceled" })),
  steer: (runId: string, text: string, interrupt: boolean) =>
    request<RunEvent>(`/v1/runs/${runId}/steer`, post({ text, interrupt })),
  artifact: (id: string) => request<Artifact>(`/v1/artifacts/${id}`),

  routines: (projectId: string) => request<Items<Routine>>(`/v1/projects/${projectId}/routines`),
  createRoutine: (
    projectId: string,
    body: { name: string; prompt: string; schedule: string; enabled: boolean; bot_member_id?: string },
  ) => request<Routine>(`/v1/projects/${projectId}/routines`, post(body)),
  updateRoutine: (id: string, body: Partial<Pick<Routine, "name" | "prompt" | "schedule" | "enabled" | "bot_member_id">>) =>
    request<Routine>(`/v1/routines/${id}`, patch(body)),
  fireRoutine: (id: string) => request<Routine>(`/v1/routines/${id}/run`, post()),

  activity: (projectId: string) => request<Page<Activity>>(`/v1/projects/${projectId}/activity${q({ limit: 50 })}`),
  usage: (projectId?: string, days = 30) =>
    request<Usage>(`${projectId ? `/v1/projects/${projectId}/usage` : "/v1/usage"}${q({ days })}`),
  presence: () => request<Presence>("/v1/presence"),
  roster: () => request<Roster>("/v1/members"),

  notifications: () =>
    request<{ items: Notification[]; unread: number; has_more: boolean }>(`/v1/me/notifications${q({ limit: 50 })}`),
  readNotification: (id: string) => request<Notification>(`/v1/notifications/${id}/read`, post()),
  readAllNotifications: () => request<{ read: number }>("/v1/me/notifications/read-all", post()),
  preferences: () => request<Preferences>("/v1/me/preferences"),
  setPreferences: (body: { mute_mentions?: boolean; mute_routines?: boolean }) =>
    request<Preferences>("/v1/me/preferences", patch(body)),
};

export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

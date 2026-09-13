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
  createTask: (projectId: string, title: string) =>
    request<import("./types").Task>(`/v1/projects/${projectId}/tasks`, {
      method: "POST",
      body: JSON.stringify({ title }),
    }),
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
  ) =>
    request(`/v1/tasks/${taskId}/handoffs`, {
      method: "POST",
      body: JSON.stringify({
        from_member_id: from,
        to_member_id: to,
        note,
      }),
    }),
  listDecisions: (projectId: string) =>
    request<{ items: import("./types").Decision[] }>(
      `/v1/projects/${projectId}/decisions`,
    ),
  createDecision: (
    projectId: string,
    body: { prompt: string; options: string[]; recommendation: string },
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
};

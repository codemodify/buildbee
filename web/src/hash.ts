export type Route =
  | { page: "home" }
  | { page: "project"; projectId: string; channelId?: string; besideId?: string }
  | { page: "task"; projectId: string; taskId: string }
  | { page: "invite"; token: string }
  | { page: "settings" };

function splitHash(raw: string): { path: string; query: URLSearchParams } {
  const hash = raw.replace(/^#/, "");
  const q = hash.indexOf("?");
  if (q < 0) return { path: hash, query: new URLSearchParams() };
  return { path: hash.slice(0, q), query: new URLSearchParams(hash.slice(q + 1)) };
}

export function parseHash(): Route {
  const pathParts = window.location.pathname.split("/").filter(Boolean);
  if (pathParts[0] === "invite" && pathParts[1]) {
    return { page: "invite", token: pathParts[1] };
  }
  const { path, query } = splitHash(window.location.hash);
  const parts = path.split("/").filter(Boolean);
  if (parts[0] === "invite" && parts[1]) {
    return { page: "invite", token: parts[1] };
  }
  if (parts[0] === "settings") {
    return { page: "settings" };
  }
  if (parts[0] === "projects" && parts[1] && parts[2] === "tasks" && parts[3]) {
    return { page: "task", projectId: parts[1], taskId: parts[3] };
  }
  if (parts[0] === "projects" && parts[1]) {
    const beside = query.get("beside") || undefined;
    return {
      page: "project",
      projectId: parts[1],
      channelId: parts[2] === "channels" ? parts[3] : undefined,
      besideId: beside && beside !== parts[1] ? beside : undefined,
    };
  }
  return { page: "home" };
}

export function projectHash(opts: {
  projectId: string;
  channelId?: string;
  besideId?: string;
}): string {
  let h = `#/projects/${opts.projectId}`;
  if (opts.channelId) h += `/channels/${opts.channelId}`;
  if (opts.besideId && opts.besideId !== opts.projectId) {
    h += `?beside=${encodeURIComponent(opts.besideId)}`;
  }
  return h;
}

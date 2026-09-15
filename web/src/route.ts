import { useEffect, useState } from "react";

/** Route is what the address bar says to show. */
export type Route =
  | { view: "home" }
  | { view: "channel"; projectId: string; channelId?: string; threadId?: string }
  | { view: "task"; projectId: string; taskId: string }
  | { view: "settings"; projectId: string }
  | { view: "status" }
  | { view: "decisions" };

export function parse(hash: string): Route {
  const [path, query = ""] = hash.replace(/^#\/?/, "").split("?");
  const params = new URLSearchParams(query);
  const threadId = params.get("thread") || undefined;
  const [p, projectId, section, id] = path.split("/").filter(Boolean);
  // #/usage and #/members were the Server's pages before #/status held them
  if (p === "status" || p === "usage" || p === "members") return { view: "status" };
  if (p === "decisions") return { view: "decisions" };
  if (p === "hi") return { view: "home" };
  if (p === "projects") return fromServerLink(path) ?? { view: "home" };
  if (p !== "p" || !projectId) return { view: "home" };
  switch (section) {
    case "c":
      return { view: "channel", projectId, channelId: id, threadId };
    // A Project's board and Decisions moved to #/status and #/decisions.
    case "tasks":
      return id ? { view: "task", projectId, taskId: id } : { view: "status" };
    case "decisions":
      return { view: "decisions" };
    case "settings":
      return { view: "settings", projectId };
    default:
      return { view: "channel", projectId, threadId };
  }
}

export function href(r: Route): string {
  const t = "threadId" in r && r.threadId ? `?thread=${r.threadId}` : "";
  switch (r.view) {
    case "home":
      return "#/";
    case "channel":
      return r.channelId ? `#/p/${r.projectId}/c/${r.channelId}${t}` : `#/p/${r.projectId}${t}`;
    case "task":
      return `#/p/${r.projectId}/tasks/${r.taskId}`;
    case "decisions":
      return "#/decisions";
    case "settings":
      return `#/p/${r.projectId}/settings`;
    case "status":
      return "#/status";
  }
}

export function go(r: Route) {
  window.location.hash = href(r);
}

/** fromServerLink turns a notification href (/projects/<id>/<kind>/<id>) into a Route. */
export function fromServerLink(link: string | undefined): Route | undefined {
  if (!link) return undefined;
  const [projects, projectId, kind, id] = link.replace(/^#/, "").split("/").filter(Boolean);
  if (projects !== "projects" || !projectId) return undefined;
  if (kind === "channels") return { view: "channel", projectId, channelId: id };
  if (kind === "threads") return { view: "channel", projectId, threadId: id };
  if (kind === "tasks" && id) return { view: "task", projectId, taskId: id };
  return { view: "channel", projectId };
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parse(window.location.hash));
  useEffect(() => {
    const on = () => setRoute(parse(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return route;
}

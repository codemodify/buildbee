import { useEffect, useState } from "react";

/** Route is what the address bar says to show. */
export type Route =
  | { view: "home" }
  | { view: "channel"; projectId: string; channelId?: string; threadId?: string }
  | { view: "tasks"; projectId: string; threadId?: string }
  | { view: "task"; projectId: string; taskId: string }
  | { view: "decisions"; projectId: string }
  | { view: "settings"; projectId: string }
  | { view: "usage"; projectId?: string };

export function parse(hash: string): Route {
  const [path, query = ""] = hash.replace(/^#\/?/, "").split("?");
  const params = new URLSearchParams(query);
  const threadId = params.get("thread") || undefined;
  const [p, projectId, section, id] = path.split("/").filter(Boolean);
  if (p === "usage") return { view: "usage" };
  if (p === "projects") return fromServerLink(path) ?? { view: "home" };
  if (p !== "p" || !projectId) return { view: "home" };
  switch (section) {
    case "c":
      return { view: "channel", projectId, channelId: id, threadId };
    case "tasks":
      return id ? { view: "task", projectId, taskId: id } : { view: "tasks", projectId, threadId };
    case "decisions":
      return { view: "decisions", projectId };
    case "settings":
      return { view: "settings", projectId };
    case "usage":
      return { view: "usage", projectId };
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
    case "tasks":
      return `#/p/${r.projectId}/tasks${t}`;
    case "task":
      return `#/p/${r.projectId}/tasks/${r.taskId}`;
    case "decisions":
      return `#/p/${r.projectId}/decisions`;
    case "settings":
      return `#/p/${r.projectId}/settings`;
    case "usage":
      return r.projectId ? `#/p/${r.projectId}/usage` : "#/usage";
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

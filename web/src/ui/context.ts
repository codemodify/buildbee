import { createContext, useContext } from "react";
import type { ProjectData } from "../store";
import type { Member, Person, Task } from "../types";

/** Ctx is what every view of a Project can read. */
export type Ctx = {
  me: Person;
  data: ProjectData;
  members: Map<string, Member>;
  tasks: Map<string, Task>;
  online: Set<string>; // Person IDs with the app open
  myMember?: Member;
  reload: () => void;
};

export const ProjectCtx = createContext<Ctx | null>(null);

export function useCtx(): Ctx {
  const c = useContext(ProjectCtx);
  if (!c) throw new Error("useCtx outside a Project");
  return c;
}

/** mentionNames are the lower-case names @mentions may use. */
export function mentionNames(members: Map<string, Member>): Set<string> {
  const out = new Set<string>();
  for (const m of members.values()) {
    out.add(m.display_name.toLowerCase().replace(/\s+/g, "_"));
    if (m.kind === "bot") out.add(m.role.toLowerCase());
  }
  return out;
}

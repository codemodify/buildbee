import { useMemo, type ReactNode } from "react";
import { useMemberIndex, useProject } from "../store";
import type { Person, Presence } from "../types";
import { ProjectCtx, type Ctx } from "./context";

/** useProjectCtx loads one Project, live, as the context its views read. */
export function useProjectCtx(projectId: string, me: Person, presence?: Presence) {
  const { data, error, reload } = useProject(projectId);
  const members = useMemberIndex(data?.members);
  const tasks = useMemo(() => new Map((data?.tasks ?? []).map((t) => [t.id, t])), [data?.tasks]);
  const online = useMemo(() => new Set((presence?.people ?? []).map((p) => p.id)), [presence]);
  const ctx: Ctx | undefined = data
    ? { me, data, members, tasks, online, myMember: data.members.find((m) => m.person_id === me.id && !m.left_at), reload }
    : undefined;
  return { ctx, error };
}

/** ProjectScope renders children inside one Project's context, once loaded. */
export function ProjectScope({ projectId, me, presence, children }: { projectId: string; me: Person; presence?: Presence; children: ReactNode }) {
  const { ctx } = useProjectCtx(projectId, me, presence);
  if (!ctx) return null;
  return <ProjectCtx.Provider value={ctx}>{children}</ProjectCtx.Provider>;
}

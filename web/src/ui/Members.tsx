import type { ReactNode } from "react";
import { api } from "../api";
import { useTopic } from "../live";
import { go } from "../route";
import { useLoad, usePresence } from "../store";
import { ago } from "../text";
import type { Roster } from "../types";
import { Avatar, ErrorNote, Pill, cx } from "./kit";

/** Members is everyone who is part of this Server: people and Bots. */
export function Members({ header }: { header: ReactNode }) {
  const { data: r, error, reload } = useLoad<Roster>(() => api.roster(), []);
  const presence = usePresence();
  useTopic("presence:server", null, reload);
  const online = new Set((presence?.people ?? []).map((p) => p.id));
  const byProject = new Map<string, Roster["bots"]>();
  for (const b of r?.bots ?? []) byProject.set(b.project_name, [...(byProject.get(b.project_name) ?? []), b]);
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-6 px-4 py-5">
          <ErrorNote>{error}</ErrorNote>
          <Group title="People" count={r?.people.length}>
            {(r?.people ?? []).map((p) => (
              <Row key={p.id}>
                <Avatar name={p.name} size={28} online={online.has(p.id)} />
                <span className="w-32 shrink-0 truncate font-medium">{p.name}</span>
                <span className="flex min-w-0 flex-wrap gap-1">
                  {p.projects.map((m) => (
                    <button key={m.member_id} type="button" onClick={() => go({ view: "channel", projectId: m.project_id })} title={m.left_at ? `Left ${ago(m.left_at)} ago` : m.role}>
                      <Pill tone={m.left_at ? "neutral" : "accent"}>
                        <span className={cx(m.left_at && "line-through")}>{m.project_name}</span>
                      </Pill>
                    </button>
                  ))}
                </span>
                <span className="ml-auto shrink-0 text-[12px] text-bb-subtle">{online.has(p.id) ? "online" : ""}</span>
              </Row>
            ))}
          </Group>
          <Group title="Bots" count={r?.bots.length}>
            {[...byProject.entries()].map(([project, bots]) => (
              <div key={project} className="py-1.5">
                <p className="px-3 pb-1 text-[12px] text-bb-subtle">{project}</p>
                {bots.map((b) => (
                  <Row key={b.id}>
                    <Avatar member={b} size={24} />
                    <span className="w-32 shrink-0 truncate font-medium">{b.display_name}</span>
                    <Pill tone="bot">{b.role}</Pill>
                    <span className="ml-auto text-[12px] text-bb-subtle">{b.agent || "any agent"}</span>
                  </Row>
                ))}
              </div>
            ))}
          </Group>
          <Group title="Recent">
            {(r?.events ?? []).length === 0 && <p className="px-3 py-2 text-[13px] text-bb-subtle">—</p>}
            {(r?.events ?? []).map((e, i) => (
              <Row key={i}>
                <span className="text-[13px]">
                  <span className="font-medium">{e.name}</span> {verb[e.action] ?? e.action}{" "}
                  <span className="font-medium">{e.project_name}</span>
                </span>
                <span className="ml-auto text-[12px] text-bb-subtle">{ago(e.at)}</span>
              </Row>
            ))}
          </Group>
        </div>
      </div>
    </section>
  );
}

const verb: Record<string, string> = { added: "was added to", left: "left", joined: "joined", created: "created" };

function Group({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return (
    <section>
      <h2 className="mb-1.5 text-[12px] font-semibold tracking-wide text-bb-subtle uppercase">
        {title} {count !== undefined && <span className="font-normal">{count}</span>}
      </h2>
      <div className="divide-y divide-bb-border rounded-lg border border-bb-border bg-bb-surface">{children}</div>
    </section>
  );
}

function Row({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-3 px-3 py-2 text-[13.5px]">{children}</div>;
}

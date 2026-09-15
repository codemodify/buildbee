import type { ReactNode } from "react";
import type { Person, Presence, Project } from "../types";
import { ProjectScope } from "./scope";
import { DecisionList } from "./Settings";

/** Decisions is every Project's open questions for people, in one place. */
export function Decisions({ me, projects, presence, header }: { me: Person; projects: Project[]; presence?: Presence; header: ReactNode }) {
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl px-4 py-5">
          {/* Projects with none render nothing; the note shows when all are empty. */}
          <div className="peer space-y-6 empty:hidden">
            {projects.map((p) => (
              <ProjectScope key={p.id} projectId={p.id} me={me} presence={presence}>
                <DecisionList />
              </ProjectScope>
            ))}
          </div>
          <p className="hidden py-10 text-center text-[13.5px] text-bb-subtle peer-empty:block">No open decisions.</p>
        </div>
      </div>
    </section>
  );
}

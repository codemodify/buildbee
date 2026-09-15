import { useState, type FormEvent } from "react";
import { api } from "../api";
import { useLoad } from "../store";
import type { BotTemplate, Project } from "../types";
import { Button, ErrorNote, Field, Sheet, cx, inputBase, inputClass } from "./kit";
import { agents } from "./Settings";

const hint: Record<string, string> = {
  scout: "plans",
  builder: "builds",
  sentry: "reviews",
  pulse: "runs Routines",
};

/**
 * AddBot adds a Bot to a Project: one of the autopilot's (Scout plans,
 * Builder builds, Sentry reviews, Pulse runs Routines) or one of your own.
 */
export function AddBot({ projects, onClose, onDone }: { projects: Project[]; onClose: () => void; onDone: () => void }) {
  const { data: templates } = useLoad(() => api.botTemplates().then((r) => r.items), []);
  const [projectId, setProjectId] = useState(projects[0]?.id ?? "");
  const [kind, setKind] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState("");
  const [agent, setAgent] = useState("");
  const [instructions, setInstructions] = useState("");
  const [err, setErr] = useState("");
  const pick = (t: BotTemplate | null) => {
    setKind(t?.role ?? "other");
    setName(t?.name ?? "");
    setRole(t?.role ?? "");
    setInstructions(t?.instructions ?? "");
  };
  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      await api.addMember(projectId, { kind: "bot", display_name: name.trim(), role: role.trim(), agent, instructions: instructions.trim() });
      onDone();
      onClose();
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : String(e2));
    }
  }
  return (
    <Sheet title="Add bot" onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        {projects.length > 1 && (
          <select id="bot-project" className={cx(inputBase, "w-full")} value={projectId} onChange={(e) => setProjectId(e.target.value)} aria-label="Project">
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        )}
        <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="Kind of bot">
          {(templates ?? []).map((t) => (
            <Chip key={t.role} on={kind === t.role} onClick={() => pick(t)} title={t.instructions}>
              {t.name} <span className="text-bb-subtle">{hint[t.role] ?? t.role}</span>
            </Chip>
          ))}
          <Chip on={kind === "other"} onClick={() => pick(null)}>
            Other
          </Chip>
        </div>
        {kind && (
          <>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Name">
                <input id="bot-name" className={inputClass} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Docs" />
              </Field>
              <Field label="Role">
                <input id="bot-role" className={inputClass} value={role} onChange={(e) => setRole(e.target.value)} placeholder="writer" />
              </Field>
            </div>
            <Field label="Agent">
              <select id="bot-agent" className={cx(inputBase, "w-full")} value={agent} onChange={(e) => setAgent(e.target.value)}>
                {agents.map((a) => (
                  <option key={a} value={a}>
                    {a || "Any agent"}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Instructions">
              <textarea
                id="bot-instructions"
                className={cx(inputClass, "min-h-20 text-[13px]")}
                value={instructions}
                onChange={(e) => setInstructions(e.target.value)}
                placeholder="You write and fix docs."
              />
            </Field>
          </>
        )}
        <ErrorNote>{err}</ErrorNote>
        <div className="flex justify-end">
          <Button tone="primary" type="submit" disabled={!projectId || !name.trim() || !role.trim()}>
            Add
          </Button>
        </div>
      </form>
    </Sheet>
  );
}

function Chip({ on, onClick, title, children }: { on: boolean; onClick: () => void; title?: string; children: React.ReactNode }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={on}
      title={title}
      onClick={onClick}
      className={cx(
        "rounded-md border px-2.5 py-1 text-[13px]",
        on ? "border-bb-accent-strong bg-bb-accent-soft font-medium" : "border-bb-border hover:bg-bb-hover",
      )}
    >
      {children}
    </button>
  );
}

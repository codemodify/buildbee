import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { go, type Route } from "../route";
import { signal } from "../signals";
import type { Channel, DM, Person, Project, Roster } from "../types";
import { cx } from "./kit";

type Place = { key: string; label: string; sub?: string; icon: string; open: () => void | Promise<void> };

/** SearchButton opens the quick switcher (Ctrl+K). */
export function SearchButton() {
  return (
    <button
      type="button"
      onClick={() => signal("switcher")}
      className="rounded-md p-1.5 text-bb-muted hover:bg-bb-hover hover:text-bb-fg"
      aria-label="Go to or search (Ctrl+K)"
      title="Go to or search (Ctrl+K)"
    >
      <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
        <circle cx="8" cy="8" r="5" />
        <path d="M12 12l3.5 3.5" />
      </svg>
    </button>
  );
}

/**
 * Switcher jumps anywhere by name: channels, DMs, people (to message),
 * Projects' settings and the pinned places; or searches messages.
 */
export function Switcher({ me, projects, onClose }: { me: Person; projects: Project[]; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [at, setAt] = useState(0);
  const [places, setPlaces] = useState<Place[]>([]);
  const list = useRef<HTMLUListElement>(null);
  useEffect(() => {
    let alive = true;
    const to = (r: Route) => () => go(r);
    Promise.all([
      Promise.all(projects.map((p) => api.channels(p.id).then((r) => r.items.map((c) => [p, c] as [Project, Channel])))).then((x) => x.flat()),
      api.directMessages().then((r) => r.items),
      api.roster(),
    ]).then(([channels, dms, roster]: [[Project, Channel][], DM[], Roster]) => {
      if (!alive) return;
      const out: Place[] = [
        { key: "status", label: "status", icon: "#", sub: "pinned", open: to({ view: "status" }) },
        { key: "decisions", label: "decisions", icon: "#", sub: "pinned", open: to({ view: "decisions" }) },
      ];
      for (const [p, c] of channels) out.push({ key: c.id, label: c.name, sub: p.name, icon: "#", open: to({ view: "channel", projectId: p.id, channelId: c.id }) });
      for (const d of dms) {
        const name = d.with.map((w) => w.name).join(", ") || d.name;
        out.push({ key: d.id, label: name, sub: "DM", icon: "@", open: to({ view: "channel", projectId: d.project_id, channelId: d.id }) });
      }
      const withDM = new Set(dms.filter((d) => d.with.length === 1).map((d) => d.with[0].person_id));
      for (const p of roster.people) {
        if (p.id === me.id || withDM.has(p.id)) continue;
        out.push({
          key: `person:${p.id}`,
          label: p.name,
          sub: "message",
          icon: "@",
          open: async () => {
            const c = await api.openDirect([p.id]);
            signal("dms");
            go({ view: "channel", projectId: c.project_id, channelId: c.id });
          },
        });
      }
      for (const p of projects) out.push({ key: `settings:${p.id}`, label: `${p.name} settings`, sub: "settings", icon: "⚙", open: to({ view: "settings", projectId: p.id }) });
      setPlaces(out);
    });
    return () => {
      alive = false;
    };
  }, [projects, me.id]);

  const shown = useMemo(() => {
    const t = q.trim().toLowerCase();
    const hits = t
      ? places
          .filter((p) => `${p.label} ${p.sub ?? ""}`.toLowerCase().includes(t))
          .sort((a, b) => Number(!a.label.toLowerCase().startsWith(t)) - Number(!b.label.toLowerCase().startsWith(t)))
      : places;
    const out = hits.slice(0, 30);
    if (t) out.push({ key: "search", label: `Search messages for “${q.trim()}”`, icon: "⌕", open: () => go({ view: "search", q: q.trim() }) });
    return out;
  }, [places, q]);
  useEffect(() => setAt(0), [q]);
  useEffect(() => {
    list.current?.querySelector<HTMLElement>(`[data-at="${at}"]`)?.scrollIntoView({ block: "nearest" });
  }, [at]);

  const choose = async (p: Place | undefined) => {
    if (!p) return;
    onClose();
    await p.open();
  };
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 px-4 pt-[12vh]" onClick={onClose}>
      <div role="dialog" aria-label="Go to" className="w-full max-w-lg overflow-hidden rounded-xl border border-bb-border bg-bb-surface shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <input
          id="switcher-q"
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            else if (e.key === "ArrowDown") {
              e.preventDefault();
              setAt((i) => Math.min(i + 1, shown.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setAt((i) => Math.max(i - 1, 0));
            } else if (e.key === "Enter") {
              e.preventDefault();
              void choose(shown[at]);
            }
          }}
          placeholder="go to a channel, person or project, or search messages"
          aria-label="Go to or search"
          className="w-full border-b border-bb-border bg-transparent px-4 py-3 text-[15px] outline-none placeholder:text-bb-subtle"
        />
        <ul ref={list} className="max-h-[50vh] overflow-y-auto py-1" role="listbox">
          {shown.map((p, i) => (
            <li key={p.key} data-at={i} role="option" aria-selected={i === at}>
              <button
                type="button"
                onMouseMove={() => setAt(i)}
                onClick={() => void choose(p)}
                className={cx("flex w-full items-center gap-2.5 px-4 py-1.5 text-left text-[13.5px]", i === at && "bg-bb-accent-soft")}
              >
                <span className="w-4 text-center text-bb-subtle">{p.icon}</span>
                <span className="truncate">{p.label}</span>
                {p.sub && <span className="ml-auto shrink-0 text-[12px] text-bb-subtle">{p.sub}</span>}
              </button>
            </li>
          ))}
        </ul>
        <p className="border-t border-bb-border px-4 py-1.5 text-[11.5px] text-bb-subtle">↑↓ to move · Enter to go · Esc to close</p>
      </div>
    </div>,
    document.body,
  );
}

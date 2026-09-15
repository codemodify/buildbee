import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { go } from "../route";
import { useLoad } from "../store";
import { ago } from "../text";
import type { SearchHit } from "../types";
import { Avatar, ErrorNote, cx, inputClass } from "./kit";

/** SearchView is message search across the Server. */
export function SearchView({ q, header }: { q: string; header: ReactNode }) {
  const [text, setText] = useState(q);
  useEffect(() => setText(q), [q]);
  const { data, error } = useLoad(() => (q.trim() ? api.search(q).then((r) => r.items) : Promise.resolve([] as SearchHit[])), [q]);
  const words = q.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-4 px-4 py-5">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              go({ view: "search", q: text.trim() });
            }}
          >
            <input id="search-q" className={inputClass} autoFocus value={text} onChange={(e) => setText(e.target.value)} placeholder="search messages" aria-label="Search messages" />
          </form>
          <ErrorNote>{error}</ErrorNote>
          {q && data && <p className="text-[12.5px] text-bb-subtle">{data.length === 0 ? "Nothing found." : `${data.length}${data.length === 50 ? "+" : ""} found`}</p>}
          <ul className="divide-y divide-bb-border overflow-hidden rounded-lg border border-bb-border bg-bb-surface empty:hidden">
            {(data ?? []).map((h) => (
              <li key={h.id}>
                <button
                  type="button"
                  onClick={() => go({ view: "channel", projectId: h.project_id ?? "", channelId: h.channel_id, threadId: h.thread_id || h.id })}
                  className="flex w-full gap-3 px-3 py-2.5 text-left hover:bg-bb-hover"
                >
                  <Avatar name={h.author_name} member={h.author_kind === "bot" ? ({ kind: "bot", display_name: h.author_name } as never) : undefined} size={28} />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline gap-2 text-[12.5px]">
                      <span className="font-semibold text-bb-fg">{h.author_name}</span>
                      <span className="truncate text-bb-subtle">
                        {h.channel_kind === "dm" ? "DM" : `#${h.channel_name}`}
                        {h.project_name ? ` · ${h.project_name}` : ""}
                        {h.thread_id ? " · in a thread" : ""}
                      </span>
                      <span className="ml-auto shrink-0 text-bb-subtle">{ago(h.created_at)}</span>
                    </span>
                    <span className="mt-0.5 line-clamp-3 block text-[13.5px] whitespace-pre-wrap">{marked(h.body, words)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

/** marked highlights the words of the query (as prefixes) in text. */
export function marked(text: string, words: string[]): ReactNode {
  if (!words.length) return text;
  const esc = words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const re = new RegExp(`(?<![\\p{L}\\p{N}])(${esc.join("|")})[\\p{L}\\p{N}]*`, "giu");
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    out.push(text.slice(last, m.index));
    out.push(
      <mark key={m.index} className={cx("rounded-sm bg-bb-accent-soft px-0.5 text-bb-fg")}>
        {m[0]}
      </mark>,
    );
    last = m.index + m[0].length;
  }
  out.push(text.slice(last));
  return out;
}

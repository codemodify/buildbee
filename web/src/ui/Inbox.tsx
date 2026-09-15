import { useState } from "react";
import { useMe } from "../me";
import { fromServerLink, go } from "../route";
import { useInbox } from "../store";
import { ago } from "../text";
import { cx } from "./kit";

/** InboxBell is the acting Person's notifications. */
export function InboxBell() {
  const me = useMe();
  const { items, unread, markAll, markOne } = useInbox(me?.id);
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="relative rounded-md p-1.5 text-bb-muted hover:bg-bb-hover hover:text-bb-fg"
        aria-label={`Notifications${unread ? `, ${unread} unread` : ""}`}
      >
        <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.5">
          <path d="M4.5 7.5a4.5 4.5 0 019 0c0 4 1.5 5 1.5 5H3s1.5-1 1.5-5zM7.5 15a1.5 1.5 0 003 0" />
        </svg>
        {unread > 0 && (
          <span className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-bb-accent-strong px-1 text-[10px] font-semibold text-stone-950">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-1 w-80 max-w-[calc(100vw-2rem)] overflow-hidden rounded-lg border border-bb-border bg-bb-surface shadow-xl">
          <div className="flex items-center justify-between border-b border-bb-border px-3 py-2">
            <p className="text-[13px] font-semibold">Notifications</p>
            {unread > 0 && (
              <button type="button" onClick={() => void markAll()} className="text-[12px] text-bb-accent hover:underline">
                Mark all read
              </button>
            )}
          </div>
          <ul className="max-h-96 divide-y divide-bb-border overflow-y-auto">
            {items.length === 0 && <li className="px-3 py-6 text-center text-[13px] text-bb-subtle">Nothing new.</li>}
            {items.map((n) => (
              <li key={n.id}>
                <button
                  type="button"
                  onClick={() => {
                    void markOne(n.id);
                    const r = fromServerLink(n.href);
                    if (r) go(r);
                    setOpen(false);
                  }}
                  className={cx("block w-full px-3 py-2 text-left hover:bg-bb-hover", !n.read_at && "bg-bb-accent-soft/40")}
                >
                  <p className="text-[13px] leading-snug font-medium">{n.title}</p>
                  {n.body && <p className="mt-0.5 line-clamp-2 text-[12px] text-bb-subtle">{n.body}</p>}
                  <p className="mt-0.5 text-[11px] text-bb-subtle">{ago(n.created_at)}</p>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}


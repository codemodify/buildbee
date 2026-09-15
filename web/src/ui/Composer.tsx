import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import type { Member } from "../types";
import { Avatar, cx } from "./kit";

/**
 * Composer writes a message: Enter sends, Shift+Enter breaks the line, and
 * typing @ suggests Members (people and Bots).
 */
export function Composer({
  members,
  placeholder,
  onSend,
  autoFocus,
  hint,
}: {
  members: Member[];
  placeholder: string;
  onSend: (body: string) => Promise<void>;
  autoFocus?: boolean;
  hint?: string;
}) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pick, setPick] = useState(0);
  const ref = useRef<HTMLTextAreaElement>(null);
  // Where the caret goes after a completion, applied before the next
  // keystroke can land.
  const caret = useRef<number | null>(null);
  useLayoutEffect(() => {
    if (caret.current === null) return;
    ref.current?.setSelectionRange(caret.current, caret.current);
    caret.current = null;
  }, [text]);

  // The @word being typed, if any.
  const mention = useMemo(() => {
    const el = ref.current;
    const upto = text.slice(0, el?.selectionStart ?? text.length);
    const m = upto.match(/(^|\s)@([\p{L}\p{N}_.-]*)$/u);
    return m ? m[2].toLowerCase() : null;
  }, [text]);
  const matches = useMemo(() => {
    if (mention === null) return [];
    return members
      .filter((m) => !m.left_at)
      .filter((m) => m.display_name.toLowerCase().startsWith(mention) || m.role.toLowerCase().startsWith(mention))
      .slice(0, 6);
  }, [mention, members]);

  function complete(m: Member) {
    const el = ref.current;
    const at = el?.selectionStart ?? text.length;
    const before = text.slice(0, at).replace(/@([\p{L}\p{N}_.-]*)$/u, `@${m.display_name.replace(/\s+/g, "_")} `);
    caret.current = before.length;
    setText(before + text.slice(at));
    setPick(0);
    el?.focus();
  }

  async function send() {
    const body = text.trim();
    if (!body || busy) return;
    setBusy(true);
    setError("");
    try {
      await onSend(body);
      setText("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      ref.current?.focus();
    }
  }

  function onKey(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (matches.length) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setPick((p) => (p + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        complete(matches[Math.min(pick, matches.length - 1)]);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  }

  return (
    <div className="relative">
      {matches.length > 0 && (
        <ul className="absolute bottom-full left-0 mb-1 w-64 overflow-hidden rounded-md border border-bb-border bg-bb-surface py-1 shadow-lg">
          {matches.map((m, i) => (
            <li key={m.id}>
              <button
                type="button"
                onMouseDown={(e) => {
                  e.preventDefault();
                  complete(m);
                }}
                className={cx("flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[13px]", i === pick && "bg-bb-hover")}
              >
                <Avatar member={m} size={20} />
                <span className="font-medium">{m.display_name}</span>
                <span className="text-bb-subtle">{m.kind === "bot" ? m.role : m.role === "owner" ? "owner" : "person"}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="rounded-lg border border-bb-border bg-bb-surface focus-within:border-bb-accent-strong">
        <textarea
          ref={ref}
          rows={Math.min(8, Math.max(1, text.split("\n").length))}
          value={text}
          autoFocus={autoFocus}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKey}
          placeholder={placeholder}
          className="block max-h-60 w-full resize-none bg-transparent px-3 py-2 text-[14px] leading-relaxed placeholder:text-bb-subtle focus:outline-none focus-visible:outline-none"
        />
        <div className="flex items-center justify-between px-2 pb-1.5">
          <span className="truncate px-1 text-[11.5px] text-bb-subtle">{error ? <span className="text-bb-danger">{error}</span> : hint}</span>
          <button
            type="button"
            onClick={() => void send()}
            disabled={!text.trim() || busy}
            className="h-7 rounded-md bg-bb-accent-strong px-3 text-[12.5px] font-medium text-stone-950 disabled:opacity-40"
          >
            Send
          </button>
        </div>
      </div>
    </div>
  );
}

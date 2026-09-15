import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { api } from "../api";
import type { FileRef, Member } from "../types";
import { Avatar, cx } from "./kit";

/**
 * Composer writes a message: Enter sends, Shift+Enter breaks the line,
 * typing @ suggests Members (people and Bots), and files come by the clip,
 * pasting or dropping.
 */
export function Composer({
  members,
  placeholder,
  onSend,
  autoFocus,
  hint,
  channelId,
}: {
  members: Member[];
  placeholder: string;
  onSend: (body: string, fileIds: string[]) => Promise<void>;
  autoFocus?: boolean;
  hint?: string;
  channelId?: string; // where files are uploaded; none: no attachments
}) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<Pending[]>([]);
  const [dragging, setDragging] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const uploading = files.some((f) => !f.ref && !f.error);
  function attach(list: FileList | File[]) {
    if (!channelId) return;
    for (const file of Array.from(list)) {
      const key = `${Date.now()}-${Math.random()}`;
      setFiles((fs) => [...fs, { key, name: file.name || "pasted", size: file.size }]);
      api.uploadFile(channelId, file).then(
        (ref) => setFiles((fs) => fs.map((f) => (f.key === key ? { ...f, ref } : f))),
        (e: unknown) => setFiles((fs) => fs.map((f) => (f.key === key ? { ...f, error: e instanceof Error ? e.message : String(e) } : f))),
      );
    }
  }
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
    const ids = files.flatMap((f) => (f.ref ? [f.ref.id] : []));
    if ((!body && !ids.length) || busy || uploading) return;
    setBusy(true);
    setError("");
    try {
      await onSend(body, ids);
      setText("");
      setFiles([]);
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
      <div
        className={cx("rounded-lg border bg-bb-surface focus-within:border-bb-accent-strong", dragging ? "border-bb-accent-strong" : "border-bb-border")}
        onDragOver={(e) => {
          if (!channelId || !e.dataTransfer.types.includes("Files")) return;
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          if (!channelId || !e.dataTransfer.files.length) return;
          e.preventDefault();
          setDragging(false);
          attach(e.dataTransfer.files);
        }}
      >
        {files.length > 0 && (
          <ul className="flex flex-wrap gap-1.5 px-2 pt-2">
            {files.map((f) => (
              <li
                key={f.key}
                className={cx(
                  "flex max-w-60 items-center gap-1.5 rounded-md border px-2 py-1 text-[12px]",
                  f.error ? "border-bb-danger text-bb-danger" : "border-bb-border text-bb-muted",
                )}
                title={f.error}
              >
                <span className="truncate">{f.name}</span>
                <span className="shrink-0 text-bb-subtle">{f.error ? "failed" : f.ref ? bytes(f.size) : "uploading…"}</span>
                <button type="button" aria-label={`Remove ${f.name}`} className="shrink-0 hover:text-bb-fg" onClick={() => setFiles((fs) => fs.filter((x) => x.key !== f.key))}>
                  ✕
                </button>
              </li>
            ))}
          </ul>
        )}
        <textarea
          ref={ref}
          rows={Math.min(8, Math.max(1, text.split("\n").length))}
          value={text}
          autoFocus={autoFocus}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKey}
          onPaste={(e) => {
            if (channelId && e.clipboardData.files.length) {
              e.preventDefault();
              attach(e.clipboardData.files);
            }
          }}
          placeholder={placeholder}
          className="block max-h-60 w-full resize-none bg-transparent px-3 py-2 text-[14px] leading-relaxed placeholder:text-bb-subtle focus:outline-none focus-visible:outline-none"
        />
        <div className="flex items-center justify-between gap-1 px-2 pb-1.5">
          {channelId && (
            <>
              <button type="button" onClick={() => picker.current?.click()} aria-label="Attach files" title="Attach files" className="rounded p-1 text-bb-subtle hover:bg-bb-hover hover:text-bb-fg">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round">
                  <path d="M10.5 4.5 5.8 9.2a1.5 1.5 0 0 0 2.1 2.1l5-5a3 3 0 0 0-4.2-4.2l-5 5a4.5 4.5 0 0 0 6.4 6.4l4.2-4.2" />
                </svg>
              </button>
              <input
                ref={picker}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files) attach(e.target.files);
                  e.target.value = "";
                }}
              />
            </>
          )}
          <span className="mr-auto truncate px-1 text-[11.5px] text-bb-subtle">{error ? <span className="text-bb-danger">{error}</span> : hint}</span>
          <button
            type="button"
            onClick={() => void send()}
            disabled={(!text.trim() && !files.some((f) => f.ref)) || busy || uploading}
            className="h-7 rounded-md bg-bb-accent-strong px-3 text-[12.5px] font-medium text-stone-950 disabled:opacity-40"
          >
            Send
          </button>
        </div>
      </div>
    </div>
  );
}

type Pending = { key: string; name: string; size: number; ref?: FileRef; error?: string };

/** bytes is a size people read: 12 KB, 3.4 MB. */
export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

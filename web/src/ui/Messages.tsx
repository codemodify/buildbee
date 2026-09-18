import { Fragment, useMemo, useState, type MouseEvent, type ReactNode } from "react";
import { api } from "../api";
import { Markdown, ago, clock } from "../text";
import type { FileRef, Message } from "../types";
import { bytes } from "./Composer";
import { mentionNames, useCtx } from "./context";
import { Avatar, Button, Confirm, Pill, cx, inputClass } from "./kit";

/** MessageList renders messages with day dividers, grouping consecutive posts by one author. */
export function MessageList({
  messages,
  onOpenThread,
  compactThread,
}: {
  messages: Message[];
  onOpenThread?: (m: Message) => void;
  compactThread?: boolean;
}) {
  let lastDay = "";
  let lastAuthor = "";
  let lastTime = 0;
  return (
    <div className="flex flex-col">
      {messages.map((m) => {
        const day = new Date(m.created_at).toDateString();
        const t = new Date(m.created_at).getTime();
        const divider = day !== lastDay;
        const grouped = !divider && m.member_id === lastAuthor && t - lastTime < 5 * 60_000 && !m.task_id;
        lastDay = day;
        lastAuthor = m.member_id;
        lastTime = t;
        return (
          <Fragment key={m.id}>
            {divider && <DayDivider iso={m.created_at} />}
            <MessageItem message={m} grouped={grouped} onOpenThread={onOpenThread} compactThread={compactThread} />
          </Fragment>
        );
      })}
    </div>
  );
}

function DayDivider({ iso }: { iso: string }) {
  const d = new Date(iso);
  const today = new Date().toDateString() === d.toDateString();
  const label = today ? "Today" : d.toLocaleDateString([], { weekday: "long", month: "short", day: "numeric" });
  return (
    <div className="my-3 flex items-center gap-3 px-4 text-[11.5px] font-medium text-bb-subtle">
      <span className="h-px flex-1 bg-bb-border" />
      {label}
      <span className="h-px flex-1 bg-bb-border" />
    </div>
  );
}

export function MessageItem({
  message: m,
  grouped,
  onOpenThread,
  compactThread,
}: {
  message: Message;
  grouped?: boolean;
  onOpenThread?: (m: Message) => void;
  compactThread?: boolean;
}) {
  const { members, online, myMember } = useCtx();
  const author = members.get(m.member_id);
  const names = useMemo(() => mentionNames(members), [members]);
  const mine = !!myMember && m.member_id === myMember.id && !m.deleted_at;
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // A message with replies opens them when clicked: the question is the
  // way in, so nothing has to be said under it. Links, buttons and
  // selecting text keep their own behaviour.
  const opens = !!onOpenThread && !compactThread && !editing && !m.deleted_at && (!!m.task_id || (m.reply_count ?? 0) > 0);
  const open = (e: MouseEvent<HTMLElement>) => {
    if (!opens || (e.target as HTMLElement).closest("a,button,input,textarea,img") || window.getSelection()?.toString()) return;
    onOpenThread?.(m);
  };
  return (
    <div className={cx("group relative flex gap-3 px-4 hover:bg-bb-hover/60", grouped ? "py-0.5" : "pt-2 pb-1")}>
      <div className="w-8 shrink-0">
        {!grouped && <Avatar member={author} size={32} online={author?.person_id ? online.has(author.person_id) : undefined} />}
        {grouped && (
          <span className="invisible block pt-1 text-right text-[10.5px] text-bb-subtle group-hover:visible">{clock(m.created_at)}</span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        {!grouped && (
          <div className="flex items-baseline gap-2">
            <span className="text-[13.5px] font-semibold">{author?.display_name ?? "Someone"}</span>
            {author?.kind === "bot" && <Pill tone="bot">{author.role}</Pill>}
            <span className="text-[11.5px] text-bb-subtle" title={new Date(m.created_at).toLocaleString()}>
              {clock(m.created_at)}
            </span>
          </div>
        )}
        {editing ? (
          <EditBox message={m} onDone={() => setEditing(false)} />
        ) : m.deleted_at ? (
          <p className="text-[13.5px] text-bb-subtle italic">Message deleted</p>
        ) : (
          <div
            className={cx("text-[14px] leading-relaxed text-bb-fg", m.edited_at && "[&_p:last-child]:inline", opens && "cursor-pointer")}
            onClick={open}
            title={opens ? "Open the replies" : undefined}
          >
            <Markdown text={m.body} mentions={names} />
            {m.edited_at && (
              <span className="ml-1 text-[11px] text-bb-subtle" title={`Edited ${new Date(m.edited_at).toLocaleString()}`}>
                (edited)
              </span>
            )}
          </div>
        )}
        {!m.deleted_at && m.files && m.files.length > 0 && <Attachments files={m.files} />}
        {!compactThread && (m.reply_count ?? 0) > 0 && onOpenThread && (
          <button
            type="button"
            onClick={() => onOpenThread(m)}
            className="mt-1 inline-flex items-center gap-2 rounded-md px-1.5 py-0.5 text-[12.5px] font-medium text-bb-accent hover:bg-bb-accent-soft"
          >
            {m.reply_count} {m.reply_count === 1 ? "reply" : "replies"}
            <span className="font-normal text-bb-subtle">last {ago(m.last_reply_at)}</span>
          </button>
        )}
      </div>
      {!editing && ((onOpenThread && !compactThread) || mine) && (
        <div className="absolute top-1 right-3 hidden overflow-hidden rounded-md border border-bb-border bg-bb-surface text-[12px] text-bb-muted shadow-sm group-focus-within:flex group-hover:flex">
          {onOpenThread && !compactThread && <Action onClick={() => onOpenThread(m)}>Reply</Action>}
          {mine && <Action onClick={() => setEditing(true)}>Edit</Action>}
          {mine && <Action onClick={() => setDeleting(true)}>Delete</Action>}
        </div>
      )}
      {deleting && (
        <Confirm title="Delete message" action="Delete" onClose={() => setDeleting(false)} onConfirm={() => api.deleteMessage(m.id).then(() => undefined)}>
          It is deleted for everyone. Replies to it stay.
        </Confirm>
      )}
    </div>
  );
}

const inlineTypes = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

/** Attachments shows images as previews and other files as downloads. */
function Attachments({ files }: { files: FileRef[] }) {
  return (
    <div className="mt-1.5 flex flex-wrap items-start gap-2">
      {files.map((f) =>
        inlineTypes.has(f.content_type) ? (
          <a key={f.id} href={f.url} target="_blank" rel="noreferrer noopener" title={f.name} className="block">
            <img src={f.url} alt={f.name} loading="lazy" className="max-h-60 max-w-full rounded-md border border-bb-border object-contain sm:max-w-sm" />
          </a>
        ) : (
          <a
            key={f.id}
            href={f.url}
            download={f.name}
            className="flex max-w-72 items-center gap-2 rounded-md border border-bb-border bg-bb-surface px-2.5 py-1.5 text-[12.5px] hover:border-bb-accent-strong/60"
          >
            <span className="text-bb-subtle">⬇</span>
            <span className="truncate font-medium">{f.name}</span>
            <span className="shrink-0 text-bb-subtle">{bytes(f.size)}</span>
          </a>
        ),
      )}
    </div>
  );
}

function Action({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="px-2 py-0.5 hover:bg-bb-hover hover:text-bb-fg">
      {children}
    </button>
  );
}

/** EditBox edits a message in place: Enter saves, Escape cancels. */
function EditBox({ message, onDone }: { message: Message; onDone: () => void }) {
  const [text, setText] = useState(message.body);
  const [err, setErr] = useState("");
  async function save() {
    if (!text.trim() || text === message.body) return onDone();
    try {
      await api.editMessage(message.id, text);
      onDone();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  return (
    <div className="mt-1 space-y-1">
      <textarea
        className={cx(inputClass, "min-h-16 text-[14px]")}
        autoFocus
        value={text}
        aria-label="Edit message"
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onDone();
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            void save();
          }
        }}
      />
      <div className="flex items-center gap-2 text-[12px] text-bb-subtle">
        <span>Enter saves · Esc cancels</span>
        {err && <span className="text-bb-danger">{err}</span>}
        <span className="ml-auto flex gap-1.5">
          <Button size="sm" onClick={onDone}>
            Cancel
          </Button>
          <Button size="sm" tone="primary" onClick={() => void save()}>
            Save
          </Button>
        </span>
      </div>
    </div>
  );
}


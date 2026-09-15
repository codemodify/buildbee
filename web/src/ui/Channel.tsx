import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import { useChannel } from "../store";
import type { Channel, Message } from "../types";
import { Composer } from "./Composer";
import { useCtx } from "./context";
import { InboxBell } from "./Inbox";
import { PrefsButton } from "./Preferences";
import { Avatar, Button, Empty, ErrorNote } from "./kit";
import { MessageList } from "./Messages";

/** ChannelView is a Channel or DM: its messages and a composer. */
export function ChannelView({
  channel,
  onOpenThread,
  header,
}: {
  channel: Channel;
  onOpenThread: (m: Message) => void;
  header: ReactNode;
}) {
  const { messages, hasMore, loadOlder, error, add } = useChannel(channel.id);
  const { data, members } = useCtx();
  const scroller = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const [loadingOlder, setLoadingOlder] = useState(false);

  // Follow new messages while the reader is at the bottom.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [messages]);
  useEffect(() => {
    atBottom.current = true;
  }, [channel.id]);

  const placeholder = channel.kind === "dm" ? `Message ${channel.name}` : `Message #${channel.name}`;
  const bot = [...members.values()].find((m) => m.kind === "bot" && !m.left_at);
  const hint = channel.kind === "dm" || !bot ? undefined : `@${bot.display_name} to delegate`;

  return (
    <section className="flex h-full min-w-0 flex-col">
      {header}
      <div
        ref={scroller}
        className="min-h-0 flex-1 overflow-y-auto"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        }}
      >
        <ErrorNote>{error}</ErrorNote>
        {hasMore && (
          <div className="py-2 text-center">
            <Button
              size="sm"
              tone="ghost"
              disabled={loadingOlder}
              onClick={async () => {
                setLoadingOlder(true);
                try {
                  await loadOlder();
                } finally {
                  setLoadingOlder(false);
                }
              }}
            >
              Show older messages
            </Button>
          </div>
        )}
        {messages.length === 0 && !error ? (
          <Empty title="No messages" />
        ) : (
          <div className="pb-2">
            <MessageList messages={messages} onOpenThread={onOpenThread} />
          </div>
        )}
      </div>
      <div className="shrink-0 px-4 pb-4">
        <Composer
          members={data.members}
          placeholder={placeholder}
          autoFocus
          onSend={async (body) => {
            atBottom.current = true;
            add(await api.postMessage(channel.id, body));
          }}
          hint={hint}
        />
      </div>
    </section>
  );
}

/** ChannelHeader names the Channel and shows who is here. */
export function ChannelHeader({ channel, onMenu }: { channel: Channel; onMenu: () => void }) {
  const { members, online, data } = useCtx();
  const project = data.project;
  const people = channel.kind === "dm" ? (channel.member_ids ?? []).map((id) => members.get(id)).filter(Boolean) : [...members.values()];
  const here = people.filter((m) => m?.person_id && online.has(m.person_id));
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-bb-border px-4">
      <MenuButton onClick={onMenu} />
      <h1 className="min-w-0 truncate text-[15px] font-semibold">{channel.kind === "dm" ? channel.name : `# ${channel.name}`}</h1>
      {project.kind !== "direct" && <span className="truncate text-[13px] text-bb-subtle">{project.name}</span>}
      <div className="ml-auto flex items-center -space-x-1.5">
        {here.slice(0, 5).map((m) => m && <Avatar key={m.id} member={m} size={22} />)}
        {here.length > 0 && <span className="pl-3 text-[12px] text-bb-subtle">{here.length} here</span>}
      </div>
      <InboxBell />
      <PrefsButton />
    </header>
  );
}

export function MenuButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="-ml-1 rounded-md p-1.5 text-bb-muted hover:bg-bb-hover" aria-label="Toggle sidebar" title="Toggle sidebar (Ctrl+\\)">
      <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.6">
        <path d="M3 5h12M3 9h12M3 13h12" />
      </svg>
    </button>
  );
}

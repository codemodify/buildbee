import { useState } from "react";
import type { Member } from "../types";
import { Button, Field, Sheet, inputClass } from "./kit";

/**
 * ConnectSheet shows the line that brings a Bot to life: buildbee-agent
 * runs the Bot's AI on whichever machine that AI is logged in to, and
 * connects back here. A Bot is offline until it runs.
 */
export function ConnectSheet({ bot, onClose }: { bot: Member; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const ai = bot.agent || "claude";
  const server = `${window.location.protocol}//${window.location.host}`;
  const command = `buildbee-agent --server ${server} --bot ${bot.id} --ai ${ai}`;
  const local = /^(localhost|127\.|\[::1\])/.test(window.location.hostname);
  return (
    <Sheet title={`Connect ${bot.display_name}`} onClose={onClose}>
      <div className="space-y-3">
        <p className="text-[13.5px] text-bb-muted">
          Run this where <span className="font-medium">{ai}</span> is installed and logged in. It stays running: {bot.display_name} is online
          while it does, and takes its work from here.
        </p>
        <Field label="Command" hint={local ? "This is a localhost address. From another machine, use the Server's LAN address." : undefined}>
          <input id="connect-command" className={inputClass} readOnly value={command} onFocus={(e) => e.currentTarget.select()} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button onClick={() => void copy(command).then(setCopied)}>{copied ? "Copied" : "Copy"}</Button>
          <Button tone="primary" onClick={onClose}>
            Done
          </Button>
        </div>
      </div>
    </Sheet>
  );
}

/** copy puts text on the clipboard, also over plain http on the LAN. */
async function copy(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const el = document.getElementById("connect-command") as HTMLInputElement | null;
    el?.select();
    return document.execCommand("copy");
  }
}

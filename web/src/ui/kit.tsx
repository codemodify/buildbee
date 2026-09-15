import { useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { Member } from "../types";

export function cx(...xs: (string | false | null | undefined)[]) {
  return xs.filter(Boolean).join(" ");
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { tone?: "primary" | "plain" | "danger" | "ghost"; size?: "sm" | "md" };

export function Button({ tone = "plain", size = "md", className, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      {...rest}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        size === "sm" ? "h-7 px-2.5 text-[12.5px]" : "h-8 px-3 text-[13px]",
        tone === "primary" && "bg-bb-accent-strong text-stone-950 hover:brightness-105",
        tone === "plain" && "border border-bb-border bg-bb-surface text-bb-fg hover:bg-bb-hover",
        tone === "danger" && "border border-bb-border bg-bb-surface text-bb-danger hover:bg-bb-danger-soft",
        tone === "ghost" && "text-bb-muted hover:bg-bb-hover hover:text-bb-fg",
        className,
      )}
    />
  );
}

/** Pill shows a state: a status, a kind, a count. */
export function Pill({ tone = "neutral", children, pulse }: { tone?: Tone; children: ReactNode; pulse?: boolean }) {
  return (
    <span
      className={cx(
        "inline-flex h-5 shrink-0 items-center gap-1 rounded-full px-2 text-[11.5px] font-medium",
        toneClass[tone],
      )}
    >
      {pulse && <span className="bb-pulse h-1.5 w-1.5 rounded-full bg-current" />}
      {children}
    </span>
  );
}

export type Tone = "neutral" | "accent" | "run" | "success" | "danger" | "bot";

const toneClass: Record<Tone, string> = {
  neutral: "bg-bb-inset text-bb-muted",
  accent: "bg-bb-accent-soft text-bb-accent",
  run: "bg-bb-run-soft text-bb-run",
  success: "bg-bb-success-soft text-bb-success",
  danger: "bg-bb-danger-soft text-bb-danger",
  bot: "bg-bb-bot-soft text-bb-bot",
};

export function runTone(status: string | undefined): Tone {
  switch (status) {
    case "running":
    case "pending":
      return "run";
    case "succeeded":
      return "success";
    case "failed":
      return "danger";
    default:
      return "neutral";
  }
}

export function taskTone(status: string | undefined): Tone {
  switch (status) {
    case "in_progress":
      return "run";
    case "done":
      return "success";
    case "canceled":
      return "neutral";
    default:
      return "accent";
  }
}

export const taskLabel: Record<string, string> = { open: "Open", in_progress: "In progress", done: "Done", canceled: "Canceled" };

/** Avatar is a Member's initials; Bots are square, people round. */
export function Avatar({ member, name, size = 32, online }: { member?: Member; name?: string; size?: number; online?: boolean }) {
  const label = member?.display_name ?? name ?? "?";
  const bot = member?.kind === "bot";
  const initials = label
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
  const hue = [...label].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7);
  return (
    <span className="relative inline-flex shrink-0" style={{ width: size, height: size }}>
      <span
        className={cx("flex h-full w-full items-center justify-center font-semibold text-white", bot ? "rounded-md" : "rounded-full")}
        style={{
          fontSize: size * 0.38,
          background: bot ? "var(--bb-bot)" : `hsl(${hue} 45% 45%)`,
          color: bot ? "var(--bb-surface)" : "white",
        }}
      >
        {initials}
      </span>
      {online !== undefined && (
        <span
          className={cx(
            "absolute -right-0.5 -bottom-0.5 h-2.5 w-2.5 rounded-full border-2 border-bb-surface",
            online ? "bg-emerald-500" : "bg-bb-border",
          )}
        />
      )}
    </span>
  );
}

export function Spinner() {
  return <span className="bb-pulse inline-block h-2 w-2 rounded-full bg-bb-subtle" aria-label="loading" />;
}

export function ErrorNote({ children }: { children: ReactNode }) {
  if (!children) return null;
  return <p className="rounded-md bg-bb-danger-soft px-3 py-2 text-[13px] text-bb-danger">{children}</p>;
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mx-auto max-w-sm py-16 text-center">
      <p className="font-medium text-bb-fg">{title}</p>
      {children && <div className="mt-1 text-[13px] text-bb-subtle">{children}</div>}
    </div>
  );
}

/**
 * Sheet is a dialog: centered on wide screens, full-height on phones. It
 * renders into the body, so a transformed ancestor (the sliding sidebar)
 * cannot shrink it to its own box.
 */
export function Sheet({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const on = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, [onClose]);
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 sm:items-center" onClick={onClose}>
      <div
        role="dialog"
        aria-label={title}
        className="max-h-[90dvh] w-full overflow-y-auto rounded-t-xl border border-bb-border bg-bb-surface p-5 shadow-xl sm:max-w-md sm:rounded-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-[15px] font-semibold">{title}</h2>
          <Button tone="ghost" size="sm" onClick={onClose} aria-label="Close">
            ✕
          </Button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-[12.5px] font-medium text-bb-muted">{label}</span>
      {children}
      {hint && <span className="block text-[12px] text-bb-subtle">{hint}</span>}
    </label>
  );
}

/** inputBase styles a field without choosing its width; inputClass fills the row. */
export const inputBase =
  "rounded-md border border-bb-border bg-bb-surface px-2.5 py-1.5 text-[13.5px] text-bb-fg placeholder:text-bb-subtle focus:border-bb-accent-strong focus:outline-none focus-visible:outline-none";
export const inputClass = `w-full ${inputBase}`;

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cx("relative h-5 w-9 shrink-0 rounded-full transition-colors", checked ? "bg-bb-accent-strong" : "bg-bb-border")}
    >
      <span className={cx("absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all", checked ? "left-[18px]" : "left-0.5")} />
    </button>
  );
}

/** Confirm asks before something that cannot be undone. */
export function Confirm({ title, children, action, onConfirm, onClose }: { title: string; children: ReactNode; action: string; onConfirm: () => Promise<void> | void; onClose: () => void }) {
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <Sheet title={title} onClose={onClose}>
      <div className="space-y-4 text-[13.5px]">
        <div className="text-bb-muted">{children}</div>
        {err && <p className="text-[12px] text-bb-danger">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            tone="danger"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await onConfirm();
                onClose();
              } catch (e) {
                setErr(e instanceof Error ? e.message : String(e));
                setBusy(false);
              }
            }}
          >
            {action}
          </Button>
        </div>
      </div>
    </Sheet>
  );
}

export type MenuItem = { label: string; danger?: boolean; onClick: () => void };

/** Menu is a ⋯ button with a short list of actions. */
export function Menu({ label, items }: { label: string; items: MenuItem[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("mousedown", away);
    window.addEventListener("keydown", esc);
    return () => {
      window.removeEventListener("mousedown", away);
      window.removeEventListener("keydown", esc);
    };
  }, [open]);
  if (items.length === 0) return null;
  return (
    <div className="relative" ref={ref}>
      <button type="button" onClick={() => setOpen((o) => !o)} aria-label={label} title={label} aria-expanded={open} className="rounded-md px-2 py-1 text-[16px] leading-none text-bb-muted hover:bg-bb-hover hover:text-bb-fg">
        ⋯
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-40 mt-1 min-w-44 overflow-hidden rounded-lg border border-bb-border bg-bb-surface py-1 shadow-xl">
          {items.map((it) => (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                it.onClick();
              }}
              className={cx("block w-full px-3 py-1.5 text-left text-[13.5px] hover:bg-bb-hover", it.danger && "text-bb-danger")}
            >
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

import type { ReactNode } from "react";

export function formatError(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e);
  try {
    const parsed = JSON.parse(raw) as { error?: string };
    if (parsed.error) return parsed.error;
  } catch {
    /* keep raw */
  }
  return raw;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="text-sm text-zinc-500">{children}</p>;
}

export function renderMentions(text: string): ReactNode {
  const parts = text.split(/(@[A-Za-z0-9_-]+)/g);
  return parts.map((part, i) =>
    part.startsWith("@") ? (
      <span key={i} className="font-medium text-amber-200">
        {part}
      </span>
    ) : (
      <span key={i}>{part}</span>
    ),
  );
}

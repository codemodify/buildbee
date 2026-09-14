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
  return <p className="text-sm text-bb-subtle">{children}</p>;
}

export function renderMentions(text: string): ReactNode {
  const parts = text.split(/(@[A-Za-z0-9_-]+)/g);
  return parts.map((part, i) =>
    part.startsWith("@") ? (
      <span key={i} className="font-medium text-bb-accent">
        {part}
      </span>
    ) : (
      <span key={i}>{part}</span>
    ),
  );
}

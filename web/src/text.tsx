import { Fragment, type ReactNode } from "react";

/**
 * Markdown is a small, safe Markdown renderer for chat and agent output:
 * fenced code, headings, lists, quotes, bold, italics, inline code, links
 * and @mentions. It builds React elements; it never injects HTML.
 */
export function Markdown({ text, mentions }: { text: string; mentions?: Set<string> }) {
  const blocks: ReactNode[] = [];
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const fence = line.match(/^```\s*([\w+-]*)\s*$/);
    if (fence) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) code.push(lines[i++]);
      i++;
      blocks.push(<CodeBlock key={blocks.length} code={code.join("\n")} lang={fence[1]} />);
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      blocks.push(
        <p key={blocks.length} className="font-semibold text-bb-fg">
          {inline(heading[2], mentions)}
        </p>,
      );
      i++;
      continue;
    }
    if (/^\s*([-*]|\d+[.)])\s+/.test(line)) {
      const ordered = /^\s*\d/.test(line);
      const items: string[] = [];
      while (i < lines.length && /^\s*([-*]|\d+[.)])\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*([-*]|\d+[.)])\s+/, ""));
        i++;
      }
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={blocks.length} className={`${ordered ? "list-decimal" : "list-disc"} space-y-0.5 pl-5`}>
          {items.map((it, j) => (
            <li key={j}>{inline(it, mentions)}</li>
          ))}
        </List>,
      );
      continue;
    }
    if (/^>\s?/.test(line)) {
      const quote: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) quote.push(lines[i++].replace(/^>\s?/, ""));
      blocks.push(
        <blockquote key={blocks.length} className="border-l-2 border-bb-border pl-3 text-bb-muted">
          {inline(quote.join("\n"), mentions)}
        </blockquote>,
      );
      continue;
    }
    if (line.trim() === "") {
      i++;
      continue;
    }
    const para: string[] = [];
    while (i < lines.length && lines[i].trim() !== "" && !/^(```|#{1,4}\s|>|\s*([-*]|\d+[.)])\s+)/.test(lines[i])) {
      para.push(lines[i++]);
    }
    blocks.push(
      <p key={blocks.length} className="whitespace-pre-wrap break-words">
        {inline(para.join("\n"), mentions)}
      </p>,
    );
  }
  return <div className="space-y-1.5">{blocks}</div>;
}

function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const diff = lang === "diff" || /^(diff --git|@@ |\+\+\+ |--- )/m.test(code);
  return (
    <pre className="overflow-x-auto rounded-md border border-bb-border bg-bb-inset px-3 py-2 font-mono text-[12.5px] leading-relaxed">
      {diff ? <DiffLines text={code} /> : code}
    </pre>
  );
}

/** DiffLines colours a unified diff. */
export function DiffLines({ text }: { text: string }) {
  return (
    <>
      {text.split("\n").map((l, i) => {
        const cls = l.startsWith("+") && !l.startsWith("+++")
          ? "text-bb-success"
          : l.startsWith("-") && !l.startsWith("---")
            ? "text-bb-danger"
            : l.startsWith("@@") || l.startsWith("diff --git")
              ? "text-bb-accent"
              : "";
        return (
          <span key={i} className={`block ${cls}`}>
            {l || " "}
          </span>
        );
      })}
    </>
  );
}

const token = /(`[^`\n]+`|\*\*[^*\n]+\*\*|\*[^*\s][^*\n]*\*|\[[^\]\n]+\]\(https?:\/\/[^)\s]+\)|https?:\/\/[^\s)>\]]+|@[\p{L}\p{N}_.-]+)/gu;

function inline(text: string, mentions?: Set<string>): ReactNode {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(token)) {
    const t = m[0];
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    last = at + t.length;
    if (t.startsWith("`")) {
      out.push(
        <code key={at} className="rounded bg-bb-inset px-1 py-px font-mono text-[0.9em]">
          {t.slice(1, -1)}
        </code>,
      );
    } else if (t.startsWith("**")) {
      out.push(<strong key={at}>{t.slice(2, -2)}</strong>);
    } else if (t.startsWith("*")) {
      out.push(<em key={at}>{t.slice(1, -1)}</em>);
    } else if (t.startsWith("[")) {
      const [, label, url] = t.match(/^\[([^\]]+)\]\((.+)\)$/) ?? [];
      out.push(<Link key={at} url={url} label={label} />);
    } else if (t.startsWith("http")) {
      out.push(<Link key={at} url={t} label={t} />);
    } else {
      const name = t.slice(1).replace(/[.\-_]+$/, "");
      const known = !mentions || mentions.has(name.toLowerCase());
      out.push(
        <Fragment key={at}>
          <span className={known ? "rounded bg-bb-accent-soft px-0.5 font-medium text-bb-accent" : ""}>@{name}</span>
          {t.slice(1 + name.length)}
        </Fragment>,
      );
    }
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function Link({ url, label }: { url: string; label: string }) {
  return (
    <a href={url} target="_blank" rel="noreferrer noopener" className="text-bb-accent underline decoration-bb-accent/40 hover:decoration-bb-accent">
      {label}
    </a>
  );
}

/** ago is a short relative time, like "3m" or "2d". */
export function ago(iso: string | undefined, now = Date.now()): string {
  if (!iso) return "";
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 45) return "now";
  if (s < 3600) return `${Math.round(s / 60)}m`;
  if (s < 86400) return `${Math.round(s / 3600)}h`;
  return `${Math.round(s / 86400)}d`;
}

/** clock is a local time of day, like 14:05. */
export function clock(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function money(cost: Record<string, number> | undefined): string {
  const parts = Object.entries(cost ?? {})
    .filter(([, v]) => v > 0)
    .map(([cur, v]) => (cur === "USD" ? `$${v.toFixed(2)}` : `${v.toFixed(2)} ${cur}`));
  return parts.join(" + ") || "—";
}

export function tokens(n: number | undefined): string {
  if (!n) return "—";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

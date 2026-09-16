/**
 * Pasting formatted text: a page, a document or a spreadsheet cell keeps
 * its shape as Markdown, which is what BuildBee shows and what agents
 * read. Anything unknown falls back to its text.
 */

const marks: Record<string, string> = { strong: "**", b: "**", em: "*", i: "*", del: "~~", s: "~~" };

/** htmlToMarkdown converts pasted HTML. It returns "" when nothing is left. */
export function htmlToMarkdown(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  doc.querySelectorAll("script,style,noscript,head").forEach((el) => el.remove());
  return tidy(block(doc.body, ""));
}

/** block renders an element's children, each block on its own lines. */
function block(el: Node, indent: string): string {
  let out = "";
  for (const node of Array.from(el.childNodes)) out += render(node, indent);
  return out;
}

function render(node: Node, indent: string): string {
  if (node.nodeType === Node.TEXT_NODE) return (node.textContent ?? "").replace(/\s+/g, " ");
  if (node.nodeType !== Node.ELEMENT_NODE) return "";
  const el = node as HTMLElement;
  const tag = el.tagName.toLowerCase();
  const inner = () => block(el, indent).trim();
  switch (tag) {
    case "br":
      return "\n";
    case "hr":
      return "\n\n---\n\n";
    case "h1":
    case "h2":
    case "h3":
    case "h4":
    case "h5":
    case "h6":
      return `\n\n${"#".repeat(Number(tag[1]))} ${inner()}\n\n`;
    case "p":
    case "div":
    case "section":
    case "article":
      return `\n\n${block(el, indent).trim()}\n\n`;
    case "blockquote":
      return `\n\n${prefix(inner(), "> ")}\n\n`;
    case "pre": {
      const code = el.textContent ?? "";
      return `\n\n\`\`\`\n${code.replace(/\n+$/, "")}\n\`\`\`\n\n`;
    }
    case "code":
      return el.closest("pre") ? (el.textContent ?? "") : "`" + (el.textContent ?? "").replace(/\s+/g, " ").trim() + "`";
    case "a": {
      const href = el.getAttribute("href") ?? "";
      const text = inner();
      if (!href || href.startsWith("javascript:")) return text;
      return text && text !== href ? `[${text}](${href})` : href;
    }
    case "img": {
      const alt = el.getAttribute("alt") ?? "image";
      const src = el.getAttribute("src") ?? "";
      // A pasted picture arrives as a file and is uploaded; a remote one
      // keeps its address, and an inline data: one only its name.
      return /^https?:/i.test(src) ? `![${alt}](${src})` : alt ? `(${alt})` : "";
    }
    case "ul":
    case "ol":
      return `\n\n${list(el, indent)}\n\n`;
    case "li":
      return block(el, indent); // handled by list()
    case "table":
      return `\n\n${table(el)}\n\n`;
    case "strong":
    case "b":
    case "em":
    case "i":
    case "del":
    case "s": {
      const text = block(el, indent).trim();
      return text ? marks[tag] + text + marks[tag] : "";
    }
    default:
      return block(el, indent);
  }
}

/** list renders ul and ol, nested lists included. */
function list(el: HTMLElement, indent: string): string {
  const ordered = el.tagName.toLowerCase() === "ol";
  const items = Array.from(el.children).filter((c) => c.tagName.toLowerCase() === "li");
  return items
    .map((li, i) => {
      const bullet = ordered ? `${i + 1}. ` : "- ";
      const nested = indent + " ".repeat(bullet.length);
      const text = tidy(block(li, nested)).replace(/\n/g, "\n" + nested);
      return indent + bullet + text;
    })
    .join("\n");
}

/** table renders a table as Markdown pipes; the first row is its header. */
function table(el: HTMLElement): string {
  const rows = Array.from(el.querySelectorAll("tr")).map((tr) =>
    Array.from(tr.querySelectorAll("th,td")).map((cell) => (cell.textContent ?? "").replace(/\s+/g, " ").trim().replace(/\|/g, "\\|")),
  );
  if (!rows.length) return "";
  const width = Math.max(...rows.map((r) => r.length));
  const line = (cells: string[]) => "| " + Array.from({ length: width }, (_, i) => cells[i] ?? "").join(" | ") + " |";
  return [line(rows[0]), "| " + Array.from({ length: width }, () => "---").join(" | ") + " |", ...rows.slice(1).map(line)].join("\n");
}

function prefix(text: string, with_: string): string {
  return text
    .split("\n")
    .map((l) => with_ + l)
    .join("\n");
}

/** tidy trims each line and leaves at most one blank line between blocks. */
function tidy(text: string): string {
  return text
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .split("\n")
    .map((l) => l.replace(/\s+$/, ""))
    .join("\n")
    .trim();
}

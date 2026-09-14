const KEY = "buildbee.workspace.panes";

export type SavedPanes = {
  left?: string;
  right?: string;
  focus?: "left" | "right";
  channels?: Record<string, string>;
};

export function loadSavedPanes(): SavedPanes | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as SavedPanes;
    if (!parsed || typeof parsed !== "object") return null;
    return parsed;
  } catch {
    return null;
  }
}

export function savePanes(state: SavedPanes): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(state));
  } catch {
    /* quota / private mode */
  }
}

const TREE_KEY = "buildbee.workspace.tree";

export function loadTreeCollapsed(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(TREE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as { collapsed?: Record<string, boolean> };
    if (!parsed?.collapsed || typeof parsed.collapsed !== "object") return {};
    return parsed.collapsed;
  } catch {
    return {};
  }
}

export function saveTreeCollapsed(collapsed: Record<string, boolean>): void {
  try {
    localStorage.setItem(TREE_KEY, JSON.stringify({ collapsed }));
  } catch {
    /* quota / private mode */
  }
}

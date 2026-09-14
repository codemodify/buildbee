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

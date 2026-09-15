/** Look is how this browser shows BuildBee: theme and control corners. */
export type Look = { theme: "system" | "light" | "dark"; corners: "rounded" | "sharp" };

const KEY = "buildbee.look";

export function loadLook(): Look {
  try {
    const l = JSON.parse(localStorage.getItem(KEY) || "{}") as Partial<Look>;
    return {
      theme: l.theme === "light" || l.theme === "dark" ? l.theme : "system",
      corners: l.corners === "sharp" ? "sharp" : "rounded",
    };
  } catch {
    return { theme: "system", corners: "rounded" };
  }
}

/** applyLook stamps the root element; the stylesheet does the rest. */
export function applyLook(l: Look) {
  const root = document.documentElement;
  if (l.theme === "system") delete root.dataset.theme;
  else root.dataset.theme = l.theme;
  if (l.corners === "sharp") root.dataset.corners = "sharp";
  else delete root.dataset.corners;
}

export function saveLook(l: Look) {
  applyLook(l);
  try {
    localStorage.setItem(KEY, JSON.stringify(l));
  } catch {
    /* private mode */
  }
}

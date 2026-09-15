import { useEffect } from "react";
import { live } from "./live";
import { fromServerLink, go } from "./route";
import type { Notification as Note } from "./types";

const KEY = "buildbee.desktop";

/** Browsers show notifications only on HTTPS or localhost. */
export const desktopPossible = typeof window !== "undefined" && window.isSecureContext && "Notification" in window;

export function desktopOn(): boolean {
  try {
    return desktopPossible && localStorage.getItem(KEY) === "1" && Notification.permission === "granted";
  } catch {
    return false;
  }
}

/** setDesktop turns desktop notifications on (asking the browser) or off. */
export async function setDesktop(on: boolean): Promise<boolean> {
  if (!desktopPossible) return false;
  if (on && Notification.permission !== "granted" && (await Notification.requestPermission()) !== "granted") return false;
  try {
    localStorage.setItem(KEY, on ? "1" : "0");
  } catch {
    /* private mode */
  }
  return on;
}

/**
 * useDesktopNotifications shows your notifications (mentions, DMs,
 * Decisions, failed Runs) as system notifications while the page is not
 * in front. Clicking one brings the page up where it points.
 */
export function useDesktopNotifications(personId: string) {
  useEffect(
    () =>
      live.subscribe(`person:${personId}`, null, (f) => {
        if (f.type !== "notification" || !desktopOn() || (document.visibilityState === "visible" && document.hasFocus())) return;
        const n = f.data as Note;
        const shown = new Notification(n.title, { body: n.body ?? "", tag: n.id });
        shown.onclick = () => {
          window.focus();
          const r = fromServerLink(n.href);
          if (r) go(r);
          shown.close();
        };
      }),
    [personId],
  );
}

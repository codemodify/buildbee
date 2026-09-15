/**
 * Signals tell other parts of the page to look again after a change they
 * would not hear about over the socket: the Project list (archiving) and
 * your DM list (closing one).
 */
export type Signal = "projects" | "dms";

export function signal(s: Signal) {
  window.dispatchEvent(new Event(`buildbee:${s}`));
}

export function onSignal(s: Signal, fn: () => void): () => void {
  const name = `buildbee:${s}`;
  window.addEventListener(name, fn);
  return () => window.removeEventListener(name, fn);
}

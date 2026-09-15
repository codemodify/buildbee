import { useEffect, useRef, useSyncExternalStore } from "react";
import type { Frame } from "./types";

type Handler = (frame: Frame) => void;

type Sub = {
  /** Last cursor seen; null subscribes to live events only. */
  cursor: number | null;
  handlers: Set<Handler>;
};

export type LiveStatus = "connecting" | "open" | "offline";

/**
 * Live is the app's one WebSocket (/v1/ws). Components subscribe to
 * topics; after a reconnect every topic is resubscribed from the last
 * cursor it saw, so nothing is missed and nothing arrives twice.
 */
class Live {
  private ws: WebSocket | null = null;
  private subs = new Map<string, Sub>();
  private status: LiveStatus = "offline";
  private listeners = new Set<() => void>();
  private retry = 0;
  private timer: number | undefined;

  getStatus = () => this.status;

  onStatus = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  private setStatus(s: LiveStatus) {
    this.status = s;
    this.listeners.forEach((fn) => fn());
  }

  start() {
    if (this.ws || this.timer !== undefined) return;
    this.connect();
  }

  private connect() {
    this.timer = undefined;
    const url = new URL("/v1/ws", window.location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(url);
    this.ws = ws;
    this.setStatus("connecting");
    ws.onopen = () => {
      this.retry = 0;
      this.setStatus("open");
      for (const [topic, sub] of this.subs) this.send(topic, sub.cursor);
    };
    ws.onmessage = (ev) => {
      let frame: Frame;
      try {
        frame = JSON.parse(String(ev.data)) as Frame;
      } catch {
        return;
      }
      const sub = this.subs.get(frame.topic);
      if (!sub) return;
      if (typeof frame.cursor === "number" && frame.cursor > 0) {
        if (sub.cursor !== null && frame.cursor <= sub.cursor) return; // seen
        sub.cursor = frame.cursor;
      }
      sub.handlers.forEach((h) => h(frame));
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.setStatus("offline");
      const delay = Math.min(30_000, 500 * 2 ** this.retry++);
      this.timer = window.setTimeout(() => this.connect(), delay);
    };
  }

  private send(topic: string, after: number | null, op: "subscribe" | "unsubscribe" = "subscribe") {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    const msg: Record<string, unknown> = { op, topic };
    if (op === "subscribe" && after !== null) msg.after = after;
    this.ws.send(JSON.stringify(msg));
  }

  /**
   * subscribe calls handler for events on topic. after replays what came
   * after that cursor (null: live events only). Returns an unsubscribe.
   */
  subscribe(topic: string, after: number | null, handler: Handler): () => void {
    this.start();
    let sub = this.subs.get(topic);
    if (!sub) {
      sub = { cursor: after, handlers: new Set() };
      this.subs.set(topic, sub);
      this.send(topic, after);
    }
    sub.handlers.add(handler);
    return () => {
      const s = this.subs.get(topic);
      if (!s) return;
      s.handlers.delete(handler);
      if (s.handlers.size === 0) {
        this.subs.delete(topic);
        this.send(topic, null, "unsubscribe");
      }
    };
  }
}

export const live = new Live();

/** useLiveStatus is the WebSocket's state, for a connection indicator. */
export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(live.onStatus, live.getStatus);
}

/**
 * useTopic subscribes while topic is set. The handler may change between
 * renders without resubscribing.
 */
export function useTopic(topic: string | null, after: number | null, handler: Handler) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    if (!topic) return;
    return live.subscribe(topic, after, (f) => ref.current(f));
    // Only the topic, and whether a cursor is known yet, resubscribe: the
    // cursor itself advances with every event.
  }, [topic, after === null]);
}

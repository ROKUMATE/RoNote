import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { subscribeImport } from "./events";
import type { ImportEvent } from "./schema";

/** Stands in for the browser's EventSource; the test plays the server. */
class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static last: FakeEventSource | undefined;

  readonly url: string;
  readyState = FakeEventSource.OPEN;
  private readonly listeners = new Map<string, ((event: MessageEvent) => void)[]>();

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.last = this;
  }

  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close() {
    this.readyState = FakeEventSource.CLOSED;
  }

  /** The server sends a status event; a closed stream receives nothing. */
  send(event: ImportEvent) {
    if (this.readyState === FakeEventSource.CLOSED) return;
    this.dispatch("status", new MessageEvent("status", { data: JSON.stringify(event) }));
  }

  /** The connection drops: the browser reconnects, or gives up. */
  drop({ giveUp }: { giveUp: boolean }) {
    if (this.readyState === FakeEventSource.CLOSED) return;
    this.readyState = giveUp ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING;
    this.dispatch("error", new MessageEvent("error"));
    if (!giveUp) this.readyState = FakeEventSource.OPEN;
  }

  private dispatch(type: string, event: MessageEvent) {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}

function server(): FakeEventSource {
  if (!FakeEventSource.last) throw new Error("no EventSource was opened");
  return FakeEventSource.last;
}

beforeEach(() => {
  FakeEventSource.last = undefined;
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("passes on status events in order and closes when the import is done", () => {
  const events: ImportEvent[] = [];
  subscribeImport("0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10", (e) => events.push(e));

  expect(server().url).toBe("/api/v1/imports/0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10/events");
  server().send({ status: "queued" });
  server().send({ status: "fetching" });
  server().send({ status: "extracting" });
  server().send({ status: "done", document_id: "doc-1" });
  // The server ends the stream after done. An open EventSource would
  // reconnect and be told "done" again.
  server().drop({ giveUp: false });
  server().send({ status: "done", document_id: "doc-1" });

  expect(events.map((e) => e.status)).toEqual(["queued", "fetching", "extracting", "done"]);
  expect(server().readyState).toBe(FakeEventSource.CLOSED);
});

test("closes when the import fails", () => {
  const events: ImportEvent[] = [];
  subscribeImport("id", (e) => events.push(e));

  server().send({ status: "fetching" });
  server().send({ status: "failed", error_code: "not_found" });

  expect(events.at(-1)).toEqual({ status: "failed", error_code: "not_found" });
  expect(server().readyState).toBe(FakeEventSource.CLOSED);
});

test("keeps following across a reconnect", () => {
  const events: ImportEvent[] = [];
  const onLost = vi.fn();
  subscribeImport("id", (e) => events.push(e), onLost);

  server().send({ status: "fetching" });
  server().drop({ giveUp: false });
  server().send({ status: "fetching" });
  server().send({ status: "done", document_id: "doc-1" });

  expect(events.map((e) => e.status)).toEqual(["fetching", "fetching", "done"]);
  expect(onLost).not.toHaveBeenCalled();
});

test("reports a stream the browser gave up on", () => {
  const onLost = vi.fn();
  subscribeImport("id", () => {}, onLost);

  server().drop({ giveUp: true });

  expect(onLost).toHaveBeenCalledOnce();
});

test("stops following when unsubscribed", () => {
  const onEvent = vi.fn();
  const unsubscribe = subscribeImport("id", onEvent);

  unsubscribe();
  server().send({ status: "fetching" });

  expect(server().readyState).toBe(FakeEventSource.CLOSED);
  expect(onEvent).not.toHaveBeenCalled();
});

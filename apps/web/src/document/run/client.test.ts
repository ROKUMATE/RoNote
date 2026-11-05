import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { openRunner, type Runner } from "./client";

const origin = "http://localhost:5174";
const hello = { v: 1, type: "hello", isolated: true };

let pending: Promise<Runner>;
let frame: HTMLIFrameElement;

beforeEach(() => {
  pending = openRunner(origin);
  const found = document.querySelector("iframe");
  if (!found) throw new Error("no runner frame");
  frame = found;
});

afterEach(async () => {
  vi.useRealTimers();
  // Settles a runner the test left waiting.
  deliver(hello);
  (await pending.catch(() => undefined))?.close();
  document.body.replaceChildren();
});

/** Delivers data to the app as a message from source on origin. */
function deliver(data: unknown, from = origin, source: Window | null = frame.contentWindow) {
  window.dispatchEvent(new MessageEvent("message", { data, origin: from, source }));
}

test("the runner's frame is hidden, sandboxed and allowed isolation", () => {
  expect(frame.src).toBe(`${origin}/?app=${encodeURIComponent(location.origin)}`);
  expect(frame.hidden).toBe(true);
  expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-same-origin");
  expect(frame.allow).toBe("cross-origin-isolated");
});

test("once the frame loads it says hello to the runner's origin only", async () => {
  const post = vi.spyOn(frame.contentWindow as Window, "postMessage").mockImplementation(() => {});
  frame.dispatchEvent(new Event("load"));
  expect(post).toHaveBeenCalledWith({ v: 1, type: "hello" }, origin);
  deliver(hello);
  const runner = await pending;
  expect(runner.isolated).toBe(true);
  runner.send({ v: 1, type: "hello" });
  expect(post).toHaveBeenLastCalledWith({ v: 1, type: "hello" }, origin);
});

test("events from another origin or window, of another version or shape, are dropped", async () => {
  const runner = vi.fn();
  void pending.then(runner);
  deliver(hello, "http://localhost:18090");
  deliver(hello, "http://127.0.0.1:5174");
  deliver(hello, origin, window);
  deliver(hello, origin, null);
  deliver({ ...hello, v: 2 });
  deliver({ ...hello, isolated: "yes" });
  deliver({ ...hello, extra: 1 });
  await Promise.resolve();
  expect(runner).not.toHaveBeenCalled();

  deliver(hello);
  await expect(pending).resolves.toMatchObject({ isolated: true });
});

test("listeners hear events until they stop, and close removes the frame", async () => {
  deliver({ ...hello, isolated: false });
  const runner = await pending;
  expect(runner.isolated).toBe(false);
  const heard = vi.fn();
  const stop = runner.listen(heard);
  deliver(hello);
  stop();
  deliver(hello);
  expect(heard).toHaveBeenCalledTimes(1);

  runner.listen(heard);
  runner.close();
  expect(document.querySelector("iframe")).toBeNull();
  deliver(hello);
  expect(heard).toHaveBeenCalledTimes(1);
});

test("a runner that never answers fails, and its frame goes", async () => {
  vi.useFakeTimers();
  const opening = openRunner(origin);
  vi.advanceTimersByTime(10_000);
  await expect(opening).rejects.toThrow("The code runner didn't answer.");
  expect(document.querySelectorAll("iframe")).toHaveLength(1);
});

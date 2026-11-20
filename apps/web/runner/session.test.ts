import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { outputLimit, runLimit } from "../src/runner/limits";
import type { RunnerEvent, Runtime } from "../src/runner/protocol";
import { type Control, claim } from "./control";
import { createSession, type RuntimeWorker, type ToWorker } from "./session";

type Fake = RuntimeWorker & { posted: ToWorker[]; terminated: boolean };

let workers: Fake[];
let events: RunnerEvent[];

beforeEach(() => {
  vi.useFakeTimers();
  workers = [];
  events = [];
});

afterEach(() => {
  vi.useRealTimers();
});

/** A session as runner/main.ts opens each, on an isolated page unless said otherwise. */
function open(runtime: Runtime = "python", isolated = true) {
  return createSession({
    runtime,
    spawn: () => {
      const worker: Fake = {
        posted: [],
        terminated: false,
        onmessage: null,
        onerror: null,
        postMessage(message) {
          this.posted.push(message);
        },
        terminate() {
          this.terminated = true;
        },
      };
      workers.push(worker);
      return worker;
    },
    isolated,
    post: (event) => events.push(event),
  });
}

/** The memory the session shares with worker. */
function controlOf(worker: Fake | undefined): Control | undefined {
  const init = worker?.posted[0];
  return init?.type === "init" ? init.control : undefined;
}

/** The worker starts the run it was sent, as a worker does: true if it may. */
const begin = (worker: Fake | undefined) => claim(controlOf(worker));

/** The worker sends data, as the code in it could. */
function say(worker: Fake | undefined, data: unknown) {
  worker?.onmessage?.(new MessageEvent("message", { data }));
}

const ready = (runtime: Runtime = "python") => ({ v: 1, type: "ready", runtime });
const run = (id: string, code = "pass", runtime: Runtime = "python") =>
  ({ v: 1, type: "run", id, runtime, code, mode: "exec" }) as const;
const done = (id: string, end: "ok" | "error" = "ok") => ({ v: 1, type: "done", id, ms: 1, end });
const kinds = () => events.map((e) => (e.type === "done" ? `done ${e.id} ${e.end}` : e.type));

test("a run starts its runtime, and its clock starts once the runtime is ready", () => {
  const python = open();
  python.run(run("a", "print(1)"));
  const [worker] = workers;
  expect(worker?.posted).toEqual([
    { type: "init", control: expect.any(Int32Array) },
    { type: "run", id: "a", code: "print(1)", mode: "exec" },
  ]);

  vi.advanceTimersByTime(40_000);
  say(worker, ready());
  say(worker, { v: 1, type: "stdout", id: "a", text: "1\n" });
  say(worker, done("a"));
  expect(events).toEqual([
    { v: 1, type: "starting", runtime: "python" },
    { v: 1, type: "ready", runtime: "python" },
    { v: 1, type: "stdout", id: "a", text: "1\n" },
    { v: 1, type: "done", id: "a", ms: 0, end: "ok" },
  ]);
});

test("warming starts the session ahead of a run, once, and the run then waits for nothing", () => {
  const python = open();
  python.warm();
  python.warm();
  expect(workers).toHaveLength(1);
  expect(workers[0]?.posted).toEqual([{ type: "init", control: expect.any(Int32Array) }]);
  say(workers[0], ready());
  vi.advanceTimersByTime(5_000);
  python.run(run("a"));
  expect(workers[0]?.posted.at(-1)).toEqual({ type: "run", id: "a", code: "pass", mode: "exec" });
  say(workers[0], done("a"));
  expect(events).toEqual([
    { v: 1, type: "starting", runtime: "python" },
    { v: 1, type: "ready", runtime: "python" },
    { v: 1, type: "done", id: "a", ms: 0, end: "ok" },
  ]);
});

test("a session names its own runtime, whatever its worker claims", () => {
  const javascript = open("javascript");
  javascript.run(run("a", "1", "javascript"));
  say(workers[0], ready("python"));
  say(workers[0], { v: 1, type: "warn", id: "a", text: "careful\n" });
  expect(events).toEqual([
    { v: 1, type: "starting", runtime: "javascript" },
    { v: 1, type: "ready", runtime: "javascript" },
    { v: 1, type: "warn", id: "a", text: "careful\n" },
  ]);
});

test("a run past the limit is interrupted, and its KeyboardInterrupt isn't shown", () => {
  const python = open();
  python.run(run("a"));
  const [worker] = workers;
  say(worker, ready());
  expect(begin(worker)).toBe(true);

  vi.advanceTimersByTime(runLimit - 1);
  expect(controlOf(worker)?.[0]).toBe(0);
  vi.advanceTimersByTime(1);
  expect(controlOf(worker)?.[0]).toBe(2);
  say(worker, { v: 1, type: "error", id: "a", text: "KeyboardInterrupt\n" });
  say(worker, done("a", "error"));
  expect(kinds()).toEqual(["starting", "ready", "done a timeout"]);
  expect(worker?.terminated).toBe(false);
});

test("a run that ignores the interrupt for a second loses its session", () => {
  const python = open();
  python.run(run("a"));
  say(workers[0], ready());
  begin(workers[0]);
  python.stop("a");
  vi.advanceTimersByTime(999);
  expect(workers[0]?.terminated).toBe(false);
  vi.advanceTimersByTime(1);
  expect(workers[0]?.terminated).toBe(true);
  expect(events.slice(2)).toEqual([
    { v: 1, type: "done", id: "a", ms: 1000, end: "stop" },
    { v: 1, type: "ended", runtime: "python", reason: "stop" },
  ]);

  python.run(run("b"));
  expect(workers).toHaveLength(2);
  expect(events.at(-1)).toEqual({ v: 1, type: "starting", runtime: "python" });
});

test("without isolation, Stop ends the session at once", () => {
  const python = open("python", false);
  python.run(run("a"));
  say(workers[0], ready());
  expect(workers[0]?.posted[0]).toEqual({ type: "init", control: undefined });
  python.stop("a");
  expect(workers[0]?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done a stop", "ended"]);
});

// Both runtimes keep the same limits, from src/runner/limits.ts: Python by
// interrupting the run, JavaScript by ending its session.
describe.each(["python", "javascript"] as const)("%s's runs keep the shared limits", (runtime) => {
  const interrupted = runtime === "python";

  test("a run stops at the run limit", () => {
    const session = open(runtime);
    session.run(run("a", "loop", runtime));
    const [worker] = workers;
    say(worker, ready(runtime));
    begin(worker);
    vi.advanceTimersByTime(runLimit - 1);
    expect(kinds()).toEqual(["starting", "ready"]);
    vi.advanceTimersByTime(1);
    if (interrupted) say(worker, done("a", "error"));
    expect(kinds().slice(2, 3)).toEqual(["done a timeout"]);
    expect(worker?.terminated).toBe(!interrupted);
  });

  test("output stops at the output limit, and the run with it", () => {
    const session = open(runtime);
    session.run(run("a", "flood", runtime));
    const [worker] = workers;
    say(worker, ready(runtime));
    begin(worker);
    say(worker, { v: 1, type: "stdout", id: "a", text: "x".repeat(600_000) });
    say(worker, { v: 1, type: "stderr", id: "a", text: "y".repeat(600_000) });
    say(worker, { v: 1, type: "stdout", id: "a", text: "more" });
    if (interrupted) say(worker, done("a", "error"));

    const output = events.flatMap((e) => (e.type === "stdout" || e.type === "stderr" ? [e] : []));
    expect(output.map((e) => [e.type, e.text.length])).toEqual([
      ["stdout", 600_000],
      ["stderr", outputLimit - 600_000],
    ]);
    expect(kinds().slice(4, 5)).toEqual(["done a output"]);
    expect(worker?.terminated).toBe(!interrupted);
  });
});

test("a check's examples count towards the output limit, each whole", () => {
  const python = open();
  python.run({ ...run("a"), mode: "check" });
  const [worker] = workers;
  say(worker, ready());
  begin(worker);
  const example = (got: string) => ({
    v: 1,
    type: "example",
    id: "a",
    source: "1\n",
    want: "1\n",
    got,
    outcome: "failed",
  });
  say(worker, example("x".repeat(600_000)));
  say(worker, example("y".repeat(600_000)));
  say(worker, done("a"));
  expect(kinds()).toEqual(["starting", "ready", "example", "done a output"]);
});

test("runs wait their turn, and Stop drops a waiting run alone", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [worker] = workers;
  say(worker, ready());
  begin(worker);
  say(worker, done("a"));
  expect(worker?.posted.map((m) => (m.type === "run" ? m.id : m.type))).toEqual(["init", "a", "b"]);

  begin(worker);
  python.run(run("c"));
  python.run(run("d"));
  python.stop("c");
  say(worker, done("b"));
  expect(kinds()).toEqual(["starting", "ready", "done a ok", "done c cancelled", "done b ok"]);
  expect(worker?.posted.map((m) => (m.type === "run" ? m.id : m.type))).toEqual([
    "init",
    "a",
    "b",
    "d",
  ]);
});

// Stop names its run, and the worker and Stop each claim a sent run by
// compare-and-swap (./control.ts): whichever comes first decides.
describe.each(["python", "javascript"] as const)("%s's Stop, in each order", (runtime) => {
  test("Stop before the worker starts the run cancels it, and keeps the session", () => {
    const session = open(runtime);
    session.run(run("a", "loop", runtime));
    session.run(run("b", "next", runtime));
    const [worker] = workers;
    say(worker, ready(runtime));
    session.stop("a");
    expect(begin(worker)).toBe(false);
    expect(controlOf(worker)?.[0]).toBe(0);
    say(worker, { v: 1, type: "done", id: "a", ms: 0, end: "cancelled" });
    expect(kinds()).toEqual(["starting", "ready", "done a cancelled"]);
    expect(worker?.terminated).toBe(false);

    // The next run starts as any other, in the same session.
    expect(worker?.posted.at(-1)).toEqual({ type: "run", id: "b", code: "next", mode: "exec" });
    expect(begin(worker)).toBe(true);
    vi.advanceTimersByTime(5_000);
    expect(worker?.terminated).toBe(false);
  });

  test("Stop during the run interrupts Python, and ends a JavaScript session", () => {
    const session = open(runtime);
    session.run(run("a", "loop", runtime));
    const [worker] = workers;
    say(worker, ready(runtime));
    expect(begin(worker)).toBe(true);
    session.stop("a");
    if (runtime === "python") {
      expect(controlOf(worker)?.[0]).toBe(2);
      say(worker, { v: 1, type: "error", id: "a", text: "KeyboardInterrupt\n" });
      say(worker, done("a", "error"));
    }
    expect(kinds()).toEqual(
      runtime === "python"
        ? ["starting", "ready", "done a stop"]
        : ["starting", "ready", "done a stop", "ended"],
    );
    expect(worker?.terminated).toBe(runtime === "javascript");
  });

  test("Stop after the run is over does nothing, to the run after it either", () => {
    const session = open(runtime);
    session.run(run("a", "quick", runtime));
    session.run(run("b", "loop", runtime));
    const [worker] = workers;
    say(worker, ready(runtime));
    begin(worker);
    say(worker, done("a"));
    begin(worker);
    session.stop("a");
    session.stop("gone");
    expect(controlOf(worker)?.[0]).toBe(0);
    expect(kinds()).toEqual(["starting", "ready", "done a ok"]);
    expect(worker?.terminated).toBe(false);
  });
});

test("a Stop that comes as the run ends never reaches the next run", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [worker] = workers;
  say(worker, ready());
  begin(worker);
  // The worker has finished a, but the session hasn't heard yet.
  python.stop("a");
  expect(controlOf(worker)?.[0]).toBe(2);
  say(worker, done("a"));
  expect(controlOf(worker)?.[0]).toBe(0);
  expect(begin(worker)).toBe(true);
  say(worker, done("b"));
  expect(kinds()).toEqual(["starting", "ready", "done a stop", "done b ok"]);
});

test("what the worker says counts only if it parses and is about the run", () => {
  const python = open();
  python.run(run("a"));
  const [worker] = workers;
  say(worker, ready());
  say(worker, ready());
  say(worker, { v: 1, type: "stdout", id: "b", text: "forged" });
  say(worker, { v: 1, type: "stdout", id: "a", text: 1 });
  say(worker, { v: 2, type: "stdout", id: "a", text: "old" });
  say(worker, { v: 1, type: "hello", isolated: false });
  say(worker, { v: 1, type: "ended", runtime: "python", reason: "crash" });
  say(worker, done("b"));
  expect(kinds()).toEqual(["starting", "ready"]);
});

test("a crash loses the run and the session, and the next run starts another", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [first] = workers;
  say(first, ready());
  first?.onerror?.(new ErrorEvent("error", { cancelable: true }));
  expect(first?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done a lost", "ended", "starting"]);
  expect(events[3]).toEqual({ v: 1, type: "ended", runtime: "python", reason: "crash" });

  const second = workers[1];
  second?.onerror?.(new ErrorEvent("error", { cancelable: true }));
  expect(events.slice(5)).toEqual([
    { v: 1, type: "done", id: "b", ms: 0, end: "lost" },
    { v: 1, type: "ended", runtime: "python", reason: "failed" },
  ]);
  say(first, ready());
  expect(events).toHaveLength(7);
});

test("Restart ends the session and the run in it", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  say(workers[0], ready());
  python.restart();
  expect(workers[0]?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done b cancelled", "done a stop", "ended"]);
  expect(events.at(-1)).toEqual({ v: 1, type: "ended", runtime: "python", reason: "restart" });
  expect(workers).toHaveLength(1);
});

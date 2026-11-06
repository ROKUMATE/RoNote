import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { outputLimit, runLimit } from "../src/runner/limits";
import type { RunnerEvent, Runtime } from "../src/runner/protocol";
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

/** A session as runner/main.ts opens each: Python interruptible when isolated, JavaScript never. */
function open(runtime: Runtime = "python", interruptible = runtime === "python") {
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
    interruptible,
    post: (event) => events.push(event),
  });
}

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
    { type: "init", interrupt: expect.any(Int32Array) },
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
  expect(workers[0]?.posted).toEqual([{ type: "init", interrupt: expect.any(Int32Array) }]);
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
  const init = worker?.posted[0];
  const interrupt = init?.type === "init" ? init.interrupt : undefined;

  vi.advanceTimersByTime(runLimit - 1);
  expect(interrupt?.[0]).toBe(0);
  vi.advanceTimersByTime(1);
  expect(interrupt?.[0]).toBe(2);
  say(worker, { v: 1, type: "error", id: "a", text: "KeyboardInterrupt\n" });
  say(worker, done("a", "error"));
  expect(kinds()).toEqual(["starting", "ready", "done a timeout"]);
  expect(worker?.terminated).toBe(false);
});

test("a run that ignores the interrupt for a second loses its session", () => {
  const python = open();
  python.run(run("a"));
  say(workers[0], ready());
  python.stop();
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
  expect(workers[0]?.posted[0]).toEqual({ type: "init", interrupt: undefined });
  python.stop();
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

test("runs wait their turn, and Stop drops those waiting", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [worker] = workers;
  say(worker, ready());
  say(worker, done("a"));
  expect(worker?.posted.map((m) => (m.type === "run" ? m.id : m.type))).toEqual(["init", "a", "b"]);

  python.run(run("c"));
  python.stop();
  say(worker, done("b", "error"));
  expect(kinds()).toEqual(["starting", "ready", "done a ok", "done c stop", "done b stop"]);
  expect(worker?.posted).toHaveLength(3);
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
  expect(kinds()).toEqual(["starting", "ready", "done b stop", "done a stop", "ended"]);
  expect(events.at(-1)).toEqual({ v: 1, type: "ended", runtime: "python", reason: "restart" });
  expect(workers).toHaveLength(1);
});

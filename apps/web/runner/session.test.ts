import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { RunnerEvent } from "../src/runner/protocol";
import { createSession, type PythonWorker, type ToWorker } from "./session";

type Fake = PythonWorker & { posted: ToWorker[]; terminated: boolean };

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

function open(isolated = true) {
  return createSession({
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

/** The worker sends data, as the code in it could. */
function say(worker: Fake | undefined, data: unknown) {
  worker?.onmessage?.(new MessageEvent("message", { data }));
}

const run = (id: string, code = "pass") => ({ v: 1, type: "run", id, code, mode: "exec" }) as const;
const done = (id: string, end: "ok" | "error" = "ok") => ({ v: 1, type: "done", id, ms: 1, end });
const kinds = () => events.map((e) => (e.type === "done" ? `done ${e.id} ${e.end}` : e.type));

test("a run starts Python, and its clock starts once Python is ready", () => {
  const python = open();
  python.run(run("a", "print(1)"));
  const [worker] = workers;
  expect(worker?.posted).toEqual([
    { type: "init", interrupt: expect.any(Int32Array) },
    { type: "run", id: "a", code: "print(1)", mode: "exec" },
  ]);

  vi.advanceTimersByTime(40_000);
  say(worker, { v: 1, type: "ready" });
  say(worker, { v: 1, type: "stdout", id: "a", text: "1\n" });
  say(worker, done("a"));
  expect(events).toEqual([
    { v: 1, type: "starting" },
    { v: 1, type: "ready" },
    { v: 1, type: "stdout", id: "a", text: "1\n" },
    { v: 1, type: "done", id: "a", ms: 0, end: "ok" },
  ]);
});

test("a run past 30 seconds is interrupted, and its KeyboardInterrupt isn't shown", () => {
  const python = open();
  python.run(run("a"));
  const [worker] = workers;
  say(worker, { v: 1, type: "ready" });
  const init = worker?.posted[0];
  const interrupt = init?.type === "init" ? init.interrupt : undefined;

  vi.advanceTimersByTime(29_999);
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
  say(workers[0], { v: 1, type: "ready" });
  python.stop();
  vi.advanceTimersByTime(999);
  expect(workers[0]?.terminated).toBe(false);
  vi.advanceTimersByTime(1);
  expect(workers[0]?.terminated).toBe(true);
  expect(events.slice(2)).toEqual([
    { v: 1, type: "done", id: "a", ms: 1000, end: "stop" },
    { v: 1, type: "ended", reason: "stop" },
  ]);

  python.run(run("b"));
  expect(workers).toHaveLength(2);
  expect(events.at(-1)).toEqual({ v: 1, type: "starting" });
});

test("without isolation, Stop ends the session at once", () => {
  const python = open(false);
  python.run(run("a"));
  say(workers[0], { v: 1, type: "ready" });
  expect(workers[0]?.posted[0]).toEqual({ type: "init", interrupt: undefined });
  python.stop();
  expect(workers[0]?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done a stop", "ended"]);
});

test("output stops at 1 MB, and the run with it", () => {
  const python = open();
  python.run(run("a"));
  const [worker] = workers;
  say(worker, { v: 1, type: "ready" });
  say(worker, { v: 1, type: "stdout", id: "a", text: "x".repeat(600_000) });
  say(worker, { v: 1, type: "stderr", id: "a", text: "y".repeat(600_000) });
  say(worker, { v: 1, type: "stdout", id: "a", text: "more" });
  say(worker, done("a", "error"));

  const output = events.flatMap((e) => (e.type === "stdout" || e.type === "stderr" ? [e] : []));
  expect(output.map((e) => [e.type, e.text.length])).toEqual([
    ["stdout", 600_000],
    ["stderr", 400_000],
  ]);
  expect(kinds().at(-1)).toBe("done a output");
});

test("runs wait their turn, and Stop drops those waiting", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [worker] = workers;
  say(worker, { v: 1, type: "ready" });
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
  say(worker, { v: 1, type: "ready" });
  say(worker, { v: 1, type: "ready" });
  say(worker, { v: 1, type: "stdout", id: "b", text: "forged" });
  say(worker, { v: 1, type: "stdout", id: "a", text: 1 });
  say(worker, { v: 2, type: "stdout", id: "a", text: "old" });
  say(worker, { v: 1, type: "hello", isolated: false });
  say(worker, { v: 1, type: "ended", reason: "crash" });
  say(worker, done("b"));
  expect(kinds()).toEqual(["starting", "ready"]);
});

test("a crash loses the run and the session, and the next run starts another", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  const [first] = workers;
  say(first, { v: 1, type: "ready" });
  first?.onerror?.(new ErrorEvent("error", { cancelable: true }));
  expect(first?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done a lost", "ended", "starting"]);
  expect(events[3]).toEqual({ v: 1, type: "ended", reason: "crash" });

  const second = workers[1];
  second?.onerror?.(new ErrorEvent("error", { cancelable: true }));
  expect(events.slice(5)).toEqual([
    { v: 1, type: "done", id: "b", ms: 0, end: "lost" },
    { v: 1, type: "ended", reason: "failed" },
  ]);
  say(first, { v: 1, type: "ready" });
  expect(events).toHaveLength(7);
});

test("Restart ends the session and the run in it", () => {
  const python = open();
  python.run(run("a"));
  python.run(run("b"));
  say(workers[0], { v: 1, type: "ready" });
  python.restart();
  expect(workers[0]?.terminated).toBe(true);
  expect(kinds()).toEqual(["starting", "ready", "done b stop", "done a stop", "ended"]);
  expect(events.at(-1)).toEqual({ v: 1, type: "ended", reason: "restart" });
  expect(workers).toHaveLength(1);
});

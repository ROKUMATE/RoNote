import type { RunnerEvent } from "../src/runner/protocol";
import { declare, type Insert, place, prepare } from "./evaluate";
import { formatLog, formatResult, formatValue, type Place } from "./format";
import { createOutput } from "./output";
import type { ToWorker } from "./session";

/*
 * JavaScript for one session (docs/adr/0008-in-browser-code-execution.md):
 * blocks share this worker's global object, as a page's scripts do. A run
 * lasts as a Node script does: until its code has settled and the timers it
 * set have fired or been cleared. Its console output, completion value and
 * uncaught errors go to the session, as devtools would show them. The
 * session ends this worker to stop a run.
 */

declare const self: DedicatedWorkerGlobalScope;

// What the reader's code could replace, kept as the worker found it.
const postMessage = self.postMessage.bind(self);
// Called by another name, eval is indirect: code runs in the global scope.
// biome-ignore lint/security/noGlobalEval: evaluating the reader's code is what this worker is for, under a policy of its own (ADR 0008).
const evaluate = globalThis.eval;
const native = {
  setTimeout: self.setTimeout.bind(self),
  clearTimeout: self.clearTimeout.bind(self),
  setInterval: self.setInterval.bind(self),
  clearInterval: self.clearInterval.bind(self),
};

function post(event: RunnerEvent) {
  postMessage(event);
}

const output = createOutput(post);

/** Each cell's lines and inserted text, so its stack positions map back to the block. */
const cells: { inserts: Insert[]; lines: number }[] = [];
const where: Place = (cell, line, column) => {
  const ran = cells[cell - 1];
  return ran && line <= ran.lines ? place(ran.inserts, line, column) : undefined;
};

let current: { id: string; failed: boolean } | undefined;

/** Reports what the run threw and nothing caught. */
function uncaught(thrown: unknown, prefix: string) {
  if (!current) return;
  current.failed = true;
  output.flush();
  post({ v: 1, type: "error", id: current.id, text: `${prefix} ${shown(thrown)}\n` });
}

/** value as text, even if showing it throws. */
function shown(value: unknown, format = formatValue): string {
  try {
    return format(value, where);
  } catch {
    return "(a value that can't be shown)";
  }
}

// An error the reader's code didn't catch is the run's to report, never a
// crash of this worker's.
self.addEventListener("error", (event) => {
  event.preventDefault();
  uncaught(event.error ?? event.message, "Uncaught");
});
self.addEventListener("unhandledrejection", (event) => {
  event.preventDefault();
  uncaught(event.reason, "Uncaught (in promise)");
});

const streams = {
  log: "stdout",
  info: "stdout",
  debug: "stdout",
  dir: "stdout",
  warn: "warn",
  error: "stderr",
} as const;
for (const [level, stream] of Object.entries(streams)) {
  console[level as keyof typeof streams] = (...args: unknown[]) => {
    if (current) output.write(stream, `${formatLog(args, where)}\n`);
  };
}

/** The timers the run's code set and that haven't fired or been cleared. */
const pending = new Set<number>();
/** Ends the wait for the run to settle, once nothing is pending. */
let settle: (() => void) | undefined;

/**
 * Settles the run if nothing is left, two tasks on: an unhandled rejection
 * is reported in a task queued after the code's last promise, which the
 * first task can overtake.
 */
function check() {
  native.setTimeout(() =>
    native.setTimeout(() => {
      if (pending.size > 0 || !settle) return;
      const done = settle;
      settle = undefined;
      done();
    }),
  );
}

type Handler = string | ((...args: unknown[]) => void);

function call(handler: Handler, args: unknown[]) {
  // A timer given code as text runs it, as the browser would.
  if (typeof handler === "string") evaluate(handler);
  else handler(...args);
}

self.setTimeout = ((handler: Handler, ms?: number, ...args: unknown[]) => {
  const id = native.setTimeout(() => {
    pending.delete(id);
    check();
    call(handler, args);
  }, ms);
  pending.add(id);
  return id;
}) as typeof setTimeout;
self.setInterval = ((handler: Handler, ms?: number, ...args: unknown[]) => {
  const id = native.setInterval(() => call(handler, args), ms);
  pending.add(id);
  return id;
}) as typeof setInterval;
self.clearTimeout = (id?: number) => {
  if (id !== undefined && pending.delete(id)) check();
  native.clearTimeout(id);
};
self.clearInterval = (id?: number) => {
  if (id !== undefined && pending.delete(id)) check();
  native.clearInterval(id);
};

async function run(id: string, code: string) {
  const started = performance.now();
  const running = { id, failed: false };
  current = running;
  output.start(id);
  try {
    const prepared = prepare(code, `cell-${cells.length + 1}.js`);
    cells.push(prepared);
    declare(prepared.names);
    const completion: [unknown] | undefined = await evaluate(prepared.source);
    if (completion && completion[0] !== undefined) {
      output.flush();
      post({ v: 1, type: "result", id, text: shown(completion[0], formatResult) });
    }
  } catch (error) {
    uncaught(error, "Uncaught");
  }
  await new Promise<void>((resolve) => {
    settle = resolve;
    check();
  });
  output.flush();
  const ms = Math.round(performance.now() - started);
  post({ v: 1, type: "done", id, ms, end: running.failed ? "error" : "ok" });
  current = undefined;
}

let runs = Promise.resolve();

self.addEventListener("message", (message: MessageEvent<ToWorker>) => {
  const request = message.data;
  if (request.type === "init") {
    post({ v: 1, type: "ready", runtime: "javascript" });
    return;
  }
  runs = runs.then(() => run(request.id, request.code));
});

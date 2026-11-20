import { limitRun, type RunLimits, type Stopped, stopGrace } from "../src/runner/limits";
import {
  parseEvent,
  type RunnerEvent,
  type RunnerRequest,
  type Runtime,
} from "../src/runner/protocol";
import { type Control, cancel, createControl, interrupt, sending } from "./control";

/*
 * One session of a runtime: a worker running it, the runs waiting for it,
 * and the limits on each (src/runner/limits.ts). The worker runs the reader's
 * code in its own realm, so whatever it sends is parsed and checked like a
 * message from another origin, and the limits run on this page's clock,
 * never the worker's say-so.
 */

type Run = Extract<RunnerRequest, { type: "run" }>;

/** What the session sends its worker. */
export type ToWorker =
  | { type: "init"; control: Control | undefined }
  | { type: "run"; id: string; code: string; mode: Run["mode"] };

/** What the session needs of a worker; the test seam. */
export type RuntimeWorker = {
  postMessage(message: ToWorker): void;
  terminate(): void;
  onmessage: ((message: MessageEvent) => void) | null;
  onerror: ((error: ErrorEvent) => void) | null;
};

export type SessionOptions = {
  runtime: Runtime;
  /** Starts a worker that runs the runtime. */
  spawn: () => RuntimeWorker;
  /**
   * Whether the page is cross-origin isolated, so the session shares memory
   * with its worker (./control.ts): then Stop cancels a run the worker
   * hasn't started, and interrupts running Python, rather than end the
   * session.
   */
  isolated: boolean;
  /** Sends an event to the app. */
  post: (event: RunnerEvent) => void;
};

export function createSession({ runtime, spawn, isolated, post }: SessionOptions) {
  let worker: { thread: RuntimeWorker; control: Control | undefined; ready: boolean } | undefined;
  const waiting: Run[] = [];
  let current:
    | (Run & { limits: RunLimits; stopped?: Stopped; cancelled?: boolean; grace?: number })
    | undefined;

  function start() {
    post({ v: 1, type: "starting", runtime });
    const thread = spawn();
    const control = isolated ? createControl() : undefined;
    const started = { thread, control, ready: false };
    worker = started;
    thread.onmessage = (message) => {
      if (worker === started) heard(message.data);
    };
    thread.onerror = (error) => {
      error.preventDefault();
      if (worker === started) end(started.ready ? "crash" : "failed");
    };
    thread.postMessage({ type: "init", control });
  }

  /** Starts the next waiting run, if none is in progress. */
  function next() {
    if (current) return;
    const run = waiting.shift();
    if (!run) return;
    if (!worker) start();
    current = { ...run, limits: limitRun(() => stop("timeout")) };
    if (worker?.ready) current.limits.start();
    if (worker?.control) sending(worker.control);
    worker?.thread.postMessage({ type: "run", id: run.id, code: run.code, mode: run.mode });
  }

  function heard(data: unknown) {
    const event = parseEvent(data);
    if (!event || !worker) return;
    if (event.type === "ready") {
      if (worker.ready) return;
      worker.ready = true;
      post({ v: 1, type: "ready", runtime });
      current?.limits.start();
      return;
    }
    if (!current || !("id" in event) || event.id !== current.id) return;
    switch (event.type) {
      case "stdout":
      case "stderr":
      case "warn":
      case "result":
      case "error": {
        // The KeyboardInterrupt that stopping raised is no news.
        if (current.stopped) return;
        const { text, full } = current.limits.take(event.text);
        post({ ...event, text });
        if (full) stop("output");
        return;
      }
      case "example": {
        if (current.stopped) return;
        // An example is whole or not at all: one past the limit stops the run.
        if (!current.limits.fits(event.source.length + event.want.length + event.got.length)) {
          stop("output");
          return;
        }
        post(event);
        return;
      }
      case "done":
        // The session knows whether it cancelled the run; the worker only says it skipped it.
        finish(
          current.cancelled
            ? "cancelled"
            : (current.stopped ?? (event.end === "error" ? "error" : "ok")),
        );
        next();
    }
  }

  function finish(end: Extract<RunnerEvent, { type: "done" }>["end"]) {
    if (!current) return;
    current.limits.end();
    clearTimeout(current.grace);
    post({ v: 1, type: "done", id: current.id, ms: current.limits.elapsed(), end });
    current = undefined;
  }

  /**
   * Stops the current run: cancels it if the worker hasn't started it,
   * interrupts it if it is Python, and else ends the session. A worker that
   * doesn't answer within stopGrace loses its session.
   */
  function stop(why: Stopped) {
    if (!current || current.stopped) return;
    current.stopped = why;
    const control = worker?.ready ? worker.control : undefined;
    if (control && cancel(control)) {
      current.cancelled = true;
    } else if (control && runtime === "python") {
      interrupt(control);
    } else {
      end("stop");
      return;
    }
    current.grace = setTimeout(() => end("stop"), stopGrace);
  }

  /** Ends the session, and the run in it; the next run starts a new one. */
  function end(reason: Extract<RunnerEvent, { type: "ended" }>["reason"]) {
    worker?.thread.terminate();
    worker = undefined;
    finish(current?.stopped ?? (reason === "restart" ? "stop" : "lost"));
    post({ v: 1, type: "ended", runtime, reason });
    next();
  }

  /** Tells the app run id was stopped before it started. */
  function cancelled(id: string) {
    post({ v: 1, type: "done", id, ms: 0, end: "cancelled" });
  }

  /** Drops the runs waiting. */
  function drop() {
    for (const run of waiting.splice(0)) cancelled(run.id);
  }

  return {
    run(request: Run) {
      waiting.push(request);
      next();
    },
    /** Starts the session ahead of its first run. */
    warm() {
      if (!worker) start();
    },
    /** Stops run id: drops it while it waits, stops it while it runs, and does nothing once it's over. */
    stop(id: string) {
      const at = waiting.findIndex((run) => run.id === id);
      if (at !== -1) {
        waiting.splice(at, 1);
        cancelled(id);
      } else if (current?.id === id) {
        stop("stop");
      }
    },
    restart() {
      drop();
      end("restart");
    },
  };
}

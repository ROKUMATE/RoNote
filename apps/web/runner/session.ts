import { limitRun, type RunLimits, type Stopped, stopGrace } from "../src/runner/limits";
import {
  parseEvent,
  type RunnerEvent,
  type RunnerRequest,
  type Runtime,
} from "../src/runner/protocol";

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
  | { type: "init"; interrupt: Int32Array | undefined }
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
  /** Whether Stop can interrupt the code, through a shared buffer, rather than end the session. */
  interruptible: boolean;
  /** Sends an event to the app. */
  post: (event: RunnerEvent) => void;
};

export function createSession({ runtime, spawn, interruptible, post }: SessionOptions) {
  let worker:
    | { thread: RuntimeWorker; interrupt: Int32Array | undefined; ready: boolean }
    | undefined;
  const waiting: Run[] = [];
  let current: (Run & { limits: RunLimits; stopped?: Stopped; grace?: number }) | undefined;

  function start() {
    post({ v: 1, type: "starting", runtime });
    const thread = spawn();
    const interrupt = interruptible ? new Int32Array(new SharedArrayBuffer(4)) : undefined;
    const started = { thread, interrupt, ready: false };
    worker = started;
    thread.onmessage = (message) => {
      if (worker === started) heard(message.data);
    };
    thread.onerror = (error) => {
      error.preventDefault();
      if (worker === started) end(started.ready ? "crash" : "failed");
    };
    thread.postMessage({ type: "init", interrupt });
  }

  /** Starts the next waiting run, if none is in progress. */
  function next() {
    if (current) return;
    const run = waiting.shift();
    if (!run) return;
    if (!worker) start();
    current = { ...run, limits: limitRun(() => stop("timeout")) };
    if (worker?.ready) current.limits.start();
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
        finish(current.stopped ?? (event.end === "error" ? "error" : "ok"));
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

  /** Stops the current run: interrupts it if it can, else ends the session. */
  function stop(why: Stopped) {
    if (!current || current.stopped) return;
    current.stopped = why;
    if (worker?.interrupt && worker.ready) {
      Atomics.store(worker.interrupt, 0, 2);
      current.grace = setTimeout(() => end("stop"), stopGrace);
    } else {
      end("stop");
    }
  }

  /** Ends the session, and the run in it; the next run starts a new one. */
  function end(reason: Extract<RunnerEvent, { type: "ended" }>["reason"]) {
    worker?.thread.terminate();
    worker = undefined;
    finish(current?.stopped ?? (reason === "restart" ? "stop" : "lost"));
    post({ v: 1, type: "ended", runtime, reason });
    next();
  }

  /** Drops the runs waiting, each told it stopped. */
  function drop() {
    for (const run of waiting.splice(0)) {
      post({ v: 1, type: "done", id: run.id, ms: 0, end: "stop" });
    }
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
    stop() {
      drop();
      stop("stop");
    },
    restart() {
      drop();
      end("restart");
    },
  };
}

import {
  outputLimit,
  parseEvent,
  type RunnerEvent,
  type RunnerRequest,
} from "../src/runner/protocol";

/*
 * One Python session: a worker running Pyodide, the runs waiting for it, and
 * the limits on each. The worker runs the reader's code in its own realm, so
 * whatever it sends is parsed and checked like a message from another origin,
 * and the limits run on this page's clock, never the worker's say-so.
 */

/** How long one run may take. */
const runLimit = 30_000;
/** How long an interrupted run has to end before its session is ended instead. */
const stopGrace = 1_000;

type Run = Extract<RunnerRequest, { type: "run" }>;
type Stopped = "stop" | "timeout" | "output";

/** What the session sends its worker. */
export type ToWorker =
  | { type: "init"; interrupt: Int32Array | undefined }
  | { type: "run"; id: string; code: string; mode: Run["mode"] };

/** What the session needs of a worker; the test seam. */
export type PythonWorker = {
  postMessage(message: ToWorker): void;
  terminate(): void;
  onmessage: ((message: MessageEvent) => void) | null;
  onerror: ((error: ErrorEvent) => void) | null;
};

export type SessionOptions = {
  /** Starts a worker that runs Python. */
  spawn: () => PythonWorker;
  /** Whether Stop can interrupt Python, rather than end the session. */
  isolated: boolean;
  /** Sends an event to the app. */
  post: (event: RunnerEvent) => void;
};

export function createSession({ spawn, isolated, post }: SessionOptions) {
  let worker:
    | { python: PythonWorker; interrupt: Int32Array | undefined; ready: boolean }
    | undefined;
  const waiting: Run[] = [];
  let current:
    | (Run & { started: number; output: number; stopped?: Stopped; timer?: number; grace?: number })
    | undefined;

  function start() {
    post({ v: 1, type: "starting" });
    const python = spawn();
    const interrupt = isolated ? new Int32Array(new SharedArrayBuffer(4)) : undefined;
    const started = { python, interrupt, ready: false };
    worker = started;
    python.onmessage = (message) => {
      if (worker === started) heard(message.data);
    };
    python.onerror = (error) => {
      error.preventDefault();
      if (worker === started) end(started.ready ? "crash" : "failed");
    };
    python.postMessage({ type: "init", interrupt });
  }

  /** Starts the next waiting run, if none is in progress. */
  function next() {
    if (current) return;
    const run = waiting.shift();
    if (!run) return;
    if (!worker) start();
    current = { ...run, started: performance.now(), output: 0 };
    if (worker?.ready) limit();
    worker?.python.postMessage({ type: "run", id: run.id, code: run.code, mode: run.mode });
  }

  /** Starts the current run's clock, once Python is ready for it. */
  function limit() {
    if (!current) return;
    current.started = performance.now();
    current.timer = setTimeout(() => stop("timeout"), runLimit);
  }

  function heard(data: unknown) {
    const event = parseEvent(data);
    if (!event || !worker) return;
    if (event.type === "ready") {
      if (worker.ready) return;
      worker.ready = true;
      post(event);
      limit();
      return;
    }
    if (!current || !("id" in event) || event.id !== current.id) return;
    switch (event.type) {
      case "stdout":
      case "stderr":
      case "result":
      case "error": {
        // The KeyboardInterrupt that stopping raised is no news.
        if (current.stopped) return;
        const room = outputLimit - current.output;
        current.output += Math.min(event.text.length, room);
        post(event.text.length <= room ? event : { ...event, text: event.text.slice(0, room) });
        if (event.text.length >= room) stop("output");
        return;
      }
      case "done":
        finish(current.stopped ?? (event.end === "error" ? "error" : "ok"));
        next();
    }
  }

  function finish(end: Extract<RunnerEvent, { type: "done" }>["end"]) {
    if (!current) return;
    clearTimeout(current.timer);
    clearTimeout(current.grace);
    const ms = Math.round(performance.now() - current.started);
    post({ v: 1, type: "done", id: current.id, ms, end });
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
    worker?.python.terminate();
    worker = undefined;
    finish(current?.stopped ?? (reason === "restart" ? "stop" : "lost"));
    post({ v: 1, type: "ended", reason });
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

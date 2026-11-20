import { loadPyodide } from "pyodide";
import type { RunnerEvent } from "../src/runner/protocol";
import cells from "./cells.py?raw";
import { type Control, claim, interruptBuffer } from "./control";
import { createOutput } from "./output";
import type { ToWorker } from "./session";

/*
 * Python for one session (docs/adr/0008-in-browser-code-execution.md):
 * Pyodide from this origin with its standard library and no packages. It
 * runs one run at a time, in the order the session sends them.
 */

declare const self: DedicatedWorkerGlobalScope;

function post(event: RunnerEvent) {
  self.postMessage(event);
}

const output = createOutput(post);

/** A stream's writer, for Pyodide: bytes in, text to the session. */
function writer(stream: "stdout" | "stderr") {
  const decoder = new TextDecoder();
  return {
    write(bytes: Uint8Array) {
      output.write(stream, decoder.decode(bytes, { stream: true }));
      return bytes.length;
    },
  };
}

async function start(control: Control | undefined) {
  const pyodide = await loadPyodide({ indexURL: import.meta.env.RONOTE_PYODIDE_URL });
  pyodide.setStdout(writer("stdout"));
  pyodide.setStderr(writer("stderr"));
  if (control) pyodide.setInterruptBuffer(interruptBuffer(control));
  const scope = pyodide.globals.get("dict")();
  pyodide.runPython(cells, { globals: scope, filename: "cells.py" });
  post({ v: 1, type: "ready", runtime: "python" });
  return { run: scope.get("run"), control };
}

/**
 * Throws error where the session hears it, which ends this session: for
 * Pyodide's own failures, such as failing to load or running out of memory,
 * never the reader's errors.
 */
function fail(error: unknown) {
  setTimeout(() => {
    throw error;
  });
}

/** Whether error is Python's KeyboardInterrupt, which Stop raises. */
function interrupted(error: unknown): boolean {
  return error instanceof Error && "type" in error && error.type === "KeyboardInterrupt";
}

let python: ReturnType<typeof start> | undefined;
let runs = Promise.resolve();

self.onmessage = (message: MessageEvent<ToWorker>) => {
  const request = message.data;
  if (request.type === "init") {
    python = start(request.control);
    python.catch(fail);
    return;
  }
  runs = runs.then(async () => {
    const started = performance.now();
    const { id } = request;
    try {
      const { run, control } = await (python ?? Promise.reject(new Error("not started")));
      if (!claim(control)) {
        post({ v: 1, type: "done", id, ms: 0, end: "cancelled" });
        return;
      }
      output.start(id);
      const outcome = await run(request.code, request.mode);
      const [value, error, examples] = outcome.toJs();
      outcome.destroy();
      output.flush();
      for (const [source, want, got, verdict] of examples ?? []) {
        post({ v: 1, type: "example", id, source, want, got, outcome: verdict });
      }
      if (error !== undefined) post({ v: 1, type: "error", id, text: error });
      else if (value !== undefined) post({ v: 1, type: "result", id, text: value });
      const ms = Math.round(performance.now() - started);
      post({ v: 1, type: "done", id, ms, end: error === undefined ? "ok" : "error" });
    } catch (error) {
      // A Stop that lands in cells.py's own code, around the reader's,
      // interrupts it there: the run is over, and the session is fine.
      if (!interrupted(error)) {
        fail(error);
        return;
      }
      output.flush();
      post({ v: 1, type: "done", id, ms: Math.round(performance.now() - started), end: "error" });
    }
  });
};

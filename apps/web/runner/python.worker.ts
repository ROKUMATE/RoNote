import { loadPyodide } from "pyodide";
import type { RunnerEvent } from "../src/runner/protocol";
import cells from "./cells.py?raw";
import type { ToWorker } from "./session";

/*
 * Python for one session (docs/adr/0008-in-browser-code-execution.md):
 * Pyodide from this origin with its standard library and no packages. It
 * runs one run at a time, in the order the session sends them.
 */

declare const self: DedicatedWorkerGlobalScope;

/** How often output goes to the session while code runs. */
const outputEvery = 50;
/** The most output one message carries, well inside the protocol's limit. */
const chunk = 65_536;

let id = "";
let pending: { stream: "stdout" | "stderr"; text: string }[] = [];
let size = 0;
let sent = 0;

function post(event: RunnerEvent) {
  self.postMessage(event);
}

function flush() {
  for (const { stream, text } of pending) {
    for (let at = 0; at < text.length; at += chunk) {
      post({ v: 1, type: stream, id, text: text.slice(at, at + chunk) });
    }
  }
  pending = [];
  size = 0;
  sent = performance.now();
}

/**
 * A stream's writer: output gathers, in order, and goes out every outputEvery
 * ms, or once there is a message's worth.
 */
function writer(stream: "stdout" | "stderr") {
  const decoder = new TextDecoder();
  return {
    write(bytes: Uint8Array) {
      const text = decoder.decode(bytes, { stream: true });
      const last = pending.at(-1);
      if (last?.stream === stream) last.text += text;
      else if (text) pending.push({ stream, text });
      size += text.length;
      if (size >= chunk || performance.now() - sent >= outputEvery) flush();
      return bytes.length;
    },
  };
}

async function start(interrupt: Int32Array | undefined) {
  const pyodide = await loadPyodide({ indexURL: "/pyodide/" });
  pyodide.setStdout(writer("stdout"));
  pyodide.setStderr(writer("stderr"));
  if (interrupt) pyodide.setInterruptBuffer(interrupt);
  const scope = pyodide.globals.get("dict")();
  pyodide.runPython(cells, { globals: scope, filename: "cells.py" });
  post({ v: 1, type: "ready" });
  return { run: scope.get("run"), interrupt };
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

let python: ReturnType<typeof start> | undefined;
let runs = Promise.resolve();

self.onmessage = (message: MessageEvent<ToWorker>) => {
  const request = message.data;
  if (request.type === "init") {
    python = start(request.interrupt);
    python.catch(fail);
    return;
  }
  runs = runs.then(async () => {
    const started = performance.now();
    try {
      const { run, interrupt } = await (python ?? Promise.reject(new Error("not started")));
      if (interrupt) interrupt[0] = 0;
      id = request.id;
      sent = performance.now();
      const outcome = await run(request.code, request.mode === "doctest");
      const [value, error] = outcome.toJs();
      outcome.destroy();
      flush();
      if (error !== undefined) post({ v: 1, type: "error", id, text: error });
      else if (value !== undefined) post({ v: 1, type: "result", id, text: value });
      const ms = Math.round(performance.now() - started);
      post({ v: 1, type: "done", id, ms, end: error === undefined ? "ok" : "error" });
    } catch (error) {
      fail(error);
    }
  });
};

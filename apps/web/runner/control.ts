/*
 * The memory a session shares with its worker when the page is isolated:
 * Pyodide's interrupt signal, and the state of the run the session sent
 * last. Stop and the worker's start each claim a sent run by
 * compare-and-swap, so exactly one wins: a Stop that comes first cancels the
 * run, which the worker then skips, and one that comes after stops it as it
 * runs. Only the session resets either word, when it sends a run.
 */

export type Control = Int32Array;

const signal = 0;
const state = 1;
const sent = 0;
const started = 1;
const cancelled = 2;
/** The signal Pyodide raises KeyboardInterrupt for. */
const sigint = 2;

export function createControl(): Control {
  return new Int32Array(new SharedArrayBuffer(2 * Int32Array.BYTES_PER_ELEMENT));
}

/** Pyodide's interrupt buffer: the signal word alone. */
export function interruptBuffer(control: Control): Int32Array {
  return control.subarray(signal, signal + 1);
}

/** The session is sending a run: nothing is interrupted, and it hasn't started. */
export function sending(control: Control) {
  Atomics.store(control, signal, 0);
  Atomics.store(control, state, sent);
}

/** The worker claims the run it was sent; false when a Stop cancelled it first. */
export function claim(control: Control | undefined): boolean {
  return !control || Atomics.compareExchange(control, state, sent, started) === sent;
}

/** The session cancels the run it sent; false when the worker has started it. */
export function cancel(control: Control): boolean {
  return Atomics.compareExchange(control, state, sent, cancelled) === sent;
}

/** The session interrupts the Python run in progress. */
export function interrupt(control: Control) {
  Atomics.store(control, signal, sigint);
}

/*
 * The limits on every run, whatever its language
 * (docs/adr/0008-in-browser-code-execution.md). The runner keeps them on its
 * own clock, never a worker's say-so; the app names them when a run hits one.
 */

/** How long one run may take. */
export const runLimit = 30_000;

/** How long an interrupted run has to end before its session is ended instead. */
export const stopGrace = 1_000;

/** The most output one run sends; past it the runner stops the run. */
export const outputLimit = 1_000_000;

/** Why a run was stopped: the reader asked, or it passed a limit. */
export type Stopped = "stop" | "timeout" | "output";

/**
 * One run's limits: its clock, from the moment its runtime is ready for it,
 * and its output so far. timeout is called when the clock runs out; output
 * past its limit is reported, for the caller to stop the run once it has
 * sent what fits.
 */
export function limitRun(timeout: () => void) {
  let started = performance.now();
  let output = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    /** Starts the clock. */
    start() {
      started = performance.now();
      timer = setTimeout(timeout, runLimit);
    },
    /** How long the run has taken, in whole milliseconds. */
    elapsed(): number {
      return Math.round(performance.now() - started);
    },
    /** The part of text that fits, and whether that reaches the limit. */
    take(text: string): { text: string; full: boolean } {
      const room = outputLimit - output;
      output += Math.min(text.length, room);
      return { text: text.length <= room ? text : text.slice(0, room), full: text.length >= room };
    },
    /** Whether output of size fits whole, counting it if it does. */
    fits(size: number): boolean {
      if (size > outputLimit - output) return false;
      output += size;
      return true;
    },
    /** Stops the clock. */
    end() {
      clearTimeout(timer);
    },
  };
}

export type RunLimits = ReturnType<typeof limitRun>;

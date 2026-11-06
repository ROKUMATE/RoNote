import type { RunnerEvent } from "../src/runner/protocol";

/*
 * Output a worker sends while code runs: gathered in order, and sent every
 * outputEvery ms or once there is a message's worth, in chunks well inside
 * the protocol's limit. The session counts it against the output limit.
 */

/** How often output goes to the session while code runs. */
const outputEvery = 50;
/** The most output one message carries. */
const chunk = 65_536;

type Stream = Extract<RunnerEvent, { type: "stdout" | "stderr" | "warn" }>["type"];

export function createOutput(post: (event: RunnerEvent) => void) {
  let id = "";
  let pending: { stream: Stream; text: string }[] = [];
  let size = 0;
  let sent = 0;

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

  return {
    /** Sends what follows as the output of run id. */
    start(run: string) {
      flush();
      id = run;
      sent = performance.now();
    },
    write(stream: Stream, text: string) {
      const last = pending.at(-1);
      if (last?.stream === stream) last.text += text;
      else if (text) pending.push({ stream, text });
      size += text.length;
      if (size >= chunk || performance.now() - sent >= outputEvery) flush();
    },
    /** Sends what has gathered, before anything else about the run. */
    flush,
  };
}

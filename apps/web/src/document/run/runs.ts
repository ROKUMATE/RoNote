import type { RunnerEvent, RunnerRequest, Runtime } from "../../runner/protocol";
import { openRunner, type Runner } from "./client";
import type { Example } from "./doctest";
import { onClose } from "./RunArea";

/*
 * One open document's runs: a session per runtime in one runner, opened on
 * the first Run, and what each block's last run printed. Never saved: it
 * goes with the page.
 */

type End = Extract<RunnerEvent, { type: "done" }>["end"];

/** Output as it came: stdout plain, the rest labelled. */
export type Chunk = { kind: "stdout" | "stderr" | "warn" | "result" | "error"; text: string };

type Mode = Extract<RunnerRequest, { type: "run" }>["mode"];

export type BlockRun = {
  runtime: Runtime;
  output: readonly Chunk[];
  /** A check's examples, in order, as they come. */
  examples?: readonly Example[];
  /** Until the run is done, it is waiting or running. */
  done?: { ms: number; end: End };
  /** News of the session from this run, such as that its state is gone. */
  note?: string;
};

/** Where a runtime's session stands: not started, starting, or ready to run. */
export type Session = "off" | "starting" | "ready";

/** Each runtime as the reader knows it. */
export const runtimeNames: Record<Runtime, string> = {
  python: "Python",
  javascript: "JavaScript",
};

const notes: Record<
  Extract<RunnerEvent, { type: "ended" }>["reason"],
  (runtime: string) => string
> = {
  restart: (name) => `${name} restarted. Earlier results are gone.`,
  stop: (name) => `Stopping restarted ${name}. Earlier results are gone.`,
  crash: (name) =>
    `${name} stopped working, perhaps out of memory, and restarts with the next run. Earlier results are gone.`,
  failed: (name) => `${name} couldn't start.`,
};

type SessionState = {
  state: Session;
  /** The block the session's news goes to: the last whose run finished. */
  news?: string | undefined;
  /** The block whose Restart was pressed, which hears that it happened. */
  restarting?: string | undefined;
};

export class DocumentRuns {
  #runner: Promise<Runner> | undefined;
  #runs = new Map<string, BlockRun>();
  /** The block of each run in progress, by run id. */
  #blocks = new Map<string, string>();
  #sessions: Record<Runtime, SessionState> = {
    python: { state: "off" },
    javascript: { state: "off" },
  };
  #count = 0;
  #listeners = new Set<() => void>();

  subscribe = (listener: () => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  session(runtime: Runtime): Session {
    return this.#sessions[runtime].state;
  }

  get(blockId: string): BlockRun | undefined {
    return this.#runs.get(blockId);
  }

  run(blockId: string, runtime: Runtime, code: string, mode: Mode) {
    this.#count += 1;
    const id = `${blockId}:${this.#count}`;
    this.#blocks.set(id, blockId);
    const session = this.#sessions[runtime];
    if (session.state === "off") session.state = "starting";
    this.#set(
      blockId,
      mode === "check" ? { runtime, output: [], examples: [] } : { runtime, output: [] },
    );
    this.#open().then(
      (runner) => runner.send({ v: 1, type: "run", id, runtime, code, mode }),
      () => {
        this.#runner = undefined;
        session.state = "off";
        this.#blocks.delete(id);
        this.#set(blockId, {
          runtime,
          output: [],
          done: { ms: 0, end: "lost" },
          note: `${runtimeNames[runtime]} couldn't start: the code runner didn't answer.`,
        });
      },
    );
  }

  stop(runtime: Runtime) {
    void this.#runner?.then((runner) => runner.send({ v: 1, type: "stop", runtime }));
  }

  /** Restarts runtime's session; the news goes to blockId, when a block's Restart asked. */
  restart(runtime: Runtime, blockId?: string) {
    this.#sessions[runtime].restarting = blockId;
    void this.#runner?.then((runner) => runner.send({ v: 1, type: "restart", runtime }));
  }

  clear(blockId: string) {
    this.#runs.delete(blockId);
    this.#changed();
  }

  close() {
    void this.#runner?.then((runner) => runner.close());
    this.#listeners.clear();
  }

  #open(): Promise<Runner> {
    if (!this.#runner) {
      this.#runner = openRunner().then((runner) => {
        runner.listen(this.#heard);
        return runner;
      });
    }
    return this.#runner;
  }

  #heard = (event: RunnerEvent) => {
    switch (event.type) {
      case "starting":
      case "ready":
        this.#sessions[event.runtime].state = event.type;
        this.#changed();
        return;
      case "ended": {
        const session = this.#sessions[event.runtime];
        session.state = "off";
        const to = (event.reason === "restart" && session.restarting) || session.news;
        const run = to === undefined ? undefined : this.#runs.get(to);
        const note = notes[event.reason](runtimeNames[event.runtime]);
        if (to !== undefined && run) this.#set(to, { ...run, note });
        else this.#changed();
        session.news = undefined;
        session.restarting = undefined;
        return;
      }
      case "hello":
        return;
    }
    const blockId = this.#blocks.get(event.id);
    const run = blockId === undefined ? undefined : this.#runs.get(blockId);
    if (blockId === undefined || !run) return;
    if (event.type === "done") {
      this.#blocks.delete(event.id);
      this.#sessions[run.runtime].news = blockId;
      this.#set(blockId, { ...run, done: { ms: event.ms, end: event.end } });
      return;
    }
    if (event.type === "example") {
      const { source, want, got, outcome } = event;
      this.#set(blockId, {
        ...run,
        examples: [...(run.examples ?? []), { source, want, got, outcome }],
      });
      return;
    }
    const last = run.output.at(-1);
    const output =
      last?.kind === event.type
        ? [...run.output.slice(0, -1), { kind: last.kind, text: last.text + event.text }]
        : [...run.output, { kind: event.type, text: event.text }];
    this.#set(blockId, { ...run, output });
  };

  #set(blockId: string, run: BlockRun) {
    this.#runs.set(blockId, run);
    this.#changed();
  }

  #changed() {
    for (const listener of this.#listeners) listener();
  }
}

const open = new Map<string, DocumentRuns>();

/** documentId's runs, from its first run until its page closes. */
export function runsOf(documentId: string): DocumentRuns {
  let runs = open.get(documentId);
  if (!runs) {
    const created = new DocumentRuns();
    open.set(documentId, created);
    onClose(documentId, () => {
      created.close();
      open.delete(documentId);
    });
    runs = created;
  }
  return runs;
}

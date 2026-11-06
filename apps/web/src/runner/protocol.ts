/*
 * The messages between the app and the runner origin
 * (docs/adr/0008-in-browser-code-execution.md). Each side checks who sent a
 * message, then parses it here, and drops anything that doesn't parse: the
 * wrong version, an unknown type, a missing, extra or mistyped field.
 */

const version = 1;

type Check<T> = (value: unknown) => value is T;

const boolean: Check<boolean> = (value) => typeof value === "boolean";

/** Text of at most max characters. */
const text =
  (max: number): Check<string> =>
  (value): value is string =>
    typeof value === "string" && value.length <= max;

/** One of values. */
const oneOf =
  <T extends string>(...values: T[]): Check<T> =>
  (value): value is T =>
    values.includes(value as T);

const duration: Check<number> = (value): value is number =>
  typeof value === "number" && Number.isFinite(value) && value >= 0;

/** A run's id: the app's choice, such as a block id. */
const id = text(200);

/** The most code one run takes: the size of the largest page RoNote fetches. */
const codeLimit = 5_000_000;

/** The most output one run sends; past it the runner stops the run. */
export const outputLimit = 1_000_000;

/** Each message type's fields besides v and type, and how to check them. */
type Shapes = Record<string, Record<string, Check<unknown>>>;

type Messages<S extends Shapes> = {
  [T in keyof S]: { v: typeof version; type: T } & {
    [F in keyof S[T]]: S[T][F] extends Check<infer V> ? V : never;
  };
}[keyof S];

const requests = {
  // Asks the runner to say it is there, and what it can do.
  hello: {},
  // Runs code in the session, starting it if need be. Runs wait their turn.
  // exec runs code whole and gives its last value; doctest runs a >>>
  // transcript statement by statement; check runs a transcript's examples
  // through doctest, comparing each with the output the page shows.
  run: { id, code: text(codeLimit), mode: oneOf("exec", "doctest", "check") },
  // Stops the run in progress and drops those waiting.
  stop: {},
  // Ends the session; the next run starts a new one.
  restart: {},
} satisfies Shapes;

const events = {
  // The runner is there. isolated: whether Stop can interrupt code, rather
  // than restart its session.
  hello: { isolated: boolean },
  // Python is loading, for the first run or the first after the session ended.
  starting: {},
  ready: {},
  stdout: { id, text: text(outputLimit) },
  stderr: { id, text: text(outputLimit) },
  // The repr of the code's last value, when it has one.
  result: { id, text: text(outputLimit) },
  // The traceback the code ended with, from the reader's code on.
  error: { id, text: text(outputLimit) },
  // One example a check ran, in order: its source, the output the page
  // expects, the output it gave, and doctest's verdict. error is an
  // exception the page didn't expect.
  example: {
    id,
    source: text(outputLimit),
    want: text(outputLimit),
    got: text(outputLimit),
    outcome: oneOf("passed", "failed", "error"),
  },
  // The run is over, after ms: it ended by itself (ok or error), was stopped
  // (stop, timeout, or output past the limit), or lost with its session.
  done: { id, ms: duration, end: oneOf("ok", "error", "stop", "timeout", "output", "lost") },
  // The session ended and its state is gone, because the reader restarted it,
  // stopping needed it, it crashed or ran out of memory, or it failed to start.
  ended: { reason: oneOf("restart", "stop", "crash", "failed") },
} satisfies Shapes;

export type RunnerRequest = Messages<typeof requests>;
export type RunnerEvent = Messages<typeof events>;

function parse<S extends Shapes>(shapes: S, data: unknown): Messages<S> | undefined {
  if (typeof data !== "object" || data === null || Object.getPrototypeOf(data) !== Object.prototype)
    return undefined;
  const message = data as Record<string, unknown>;
  if (message.v !== version || typeof message.type !== "string") return undefined;
  if (!Object.hasOwn(shapes, message.type)) return undefined;
  const fields = shapes[message.type] ?? {};
  if (Object.keys(message).length !== Object.keys(fields).length + 2) return undefined;
  for (const [name, check] of Object.entries(fields)) {
    if (!Object.hasOwn(message, name) || !check(message[name])) return undefined;
  }
  return message as Messages<S>;
}

/** data as a request to the runner, or undefined if it isn't one. */
export function parseRequest(data: unknown): RunnerRequest | undefined {
  return parse(requests, data);
}

/** data as an event from the runner, or undefined if it isn't one. */
export function parseEvent(data: unknown): RunnerEvent | undefined {
  return parse(events, data);
}

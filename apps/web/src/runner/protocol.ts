/*
 * The messages between the app and the runner origin
 * (docs/adr/0008-in-browser-code-execution.md). Each side checks who sent a
 * message, then parses it here, and drops anything that doesn't parse: the
 * wrong version, an unknown type, a missing, extra or mistyped field.
 */

const version = 1;

type Check<T> = (value: unknown) => value is T;

const boolean: Check<boolean> = (value) => typeof value === "boolean";

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
} satisfies Shapes;

const events = {
  // The runner is there. isolated: whether Stop can interrupt code, rather
  // than restart its session.
  hello: { isolated: boolean },
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

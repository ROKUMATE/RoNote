/*
 * JavaScript values as text, close to how browser devtools show them
 * (docs/adr/0008-in-browser-code-execution.md): what console.log prints, a
 * block's completion value, and an uncaught error. Never runs the reader's
 * getters or toString, and never loops on a cycle.
 */

/** How deep objects show before they fold to {…}. */
const depthLimit = 2;
/** The most entries one object, array, Map or Set shows. */
const entryLimit = 100;
/** The widest a value shows on one line before it breaks into an entry a line. */
const width = 72;

/**
 * A position in a block's cell, as the same place in the block: [line,
 * column]; or nothing, for a position in what the runner wrapped it in.
 */
export type Place = (cell: number, line: number, column: number) => [number, number] | undefined;

const unmoved: Place = (_, line, column) => [line, column];

const identifier = /^[A-Za-z_$][\w$]*$/;

function quote(text: string): string {
  return `'${text.replaceAll("\\", "\\\\").replaceAll("'", "\\'").replaceAll("\n", "\\n")}'`;
}

function isError(value: object): value is Error {
  return value instanceof Error || Object.prototype.toString.call(value) === "[object Error]";
}

/** The class a value was made by, unless it is a plain object. */
function className(value: object): string {
  const proto = Object.getPrototypeOf(value);
  if (proto === null) return "";
  const name = Object.getOwnPropertyDescriptor(proto, "constructor")?.value?.name;
  return typeof name === "string" && name !== "Object" ? name : "";
}

/** A function inside something else: its name, as devtools previews it. */
function functionLabel(fn: (...args: never[]) => unknown): string {
  const source = Function.prototype.toString.call(fn);
  return source.startsWith("class") ? `class ${fn.name}`.trimEnd() : `ƒ ${fn.name}()`;
}

/** A composite's entries, on one line when they fit, else one a line. */
function wrap(open: string, entries: readonly string[], close: string, indent: number): string {
  if (entries.length === 0) return `${open}${close}`;
  const line = `${open}${entries.join(", ")}${close}`;
  if (line.length + indent <= width && !entries.some((e) => e.includes("\n"))) return line;
  const inner = entries.map((e) => `  ${e.replaceAll("\n", "\n  ")}`);
  return `${open.trimEnd()}\n${inner.join(",\n")}\n${close.trimStart()}`;
}

function limited<T>(items: readonly T[], show: (item: T) => string): string[] {
  const shown = items.slice(0, entryLimit).map(show);
  if (items.length > entryLimit) shown.push(`…${items.length - entryLimit} more`);
  return shown;
}

/** A key as an object literal would write it. */
function key(name: string | symbol): string {
  if (typeof name === "symbol") return `[${name.toString()}]`;
  return identifier.test(name) ? name : quote(name);
}

/** value inside something else: strings quoted, objects past depthLimit folded. */
function inspect(
  value: unknown,
  depth: number,
  ancestors: readonly object[],
  place: Place,
): string {
  switch (typeof value) {
    case "string":
      return quote(value);
    case "number":
      return Object.is(value, -0) ? "-0" : String(value);
    case "bigint":
      return `${value}n`;
    case "boolean":
    case "undefined":
      return String(value);
    case "symbol":
      return value.toString();
    case "function":
      return functionLabel(value as (...args: never[]) => unknown);
  }
  if (value === null) return "null";
  const object = value as object;
  if (ancestors.includes(object)) return "[Circular]";
  if (isError(object)) return firstLine(object);
  if (object instanceof Date)
    return Number.isNaN(object.getTime()) ? "Invalid Date" : object.toString();
  if (object instanceof RegExp) return RegExp.prototype.toString.call(object);
  if (object instanceof Promise) return "Promise {…}";
  if (object instanceof WeakMap || object instanceof WeakSet) return `${className(object)} {}`;

  const inside = [...ancestors, object];
  const indent = depth * 2;
  const nested = (v: unknown) => inspect(v, depth + 1, inside, place);
  const folded = depth >= depthLimit;

  if (Array.isArray(object)) {
    if (folded) return `Array(${object.length})`;
    return wrap(
      "[",
      limited(arrayEntries(object), (e) => e(nested)),
      "]",
      indent,
    );
  }
  if (ArrayBuffer.isView(object) && !(object instanceof DataView)) {
    const items: unknown[] = Array.from(object as unknown as ArrayLike<number | bigint>);
    const label = `${className(object)}(${items.length})`;
    if (folded) return label;
    return wrap(`${label} [`, limited(items, nested), "]", indent);
  }
  if (object instanceof Map) {
    const label = `${className(object) || "Map"}(${object.size})`;
    if (folded) return label;
    const entries = limited([...object], ([k, v]) => `${nested(k)} => ${nested(v)}`);
    return wrap(`${label} {`, entries, "}", indent);
  }
  if (object instanceof Set) {
    const label = `${className(object) || "Set"}(${object.size})`;
    if (folded) return label;
    return wrap(`${label} {`, limited([...object], nested), "}", indent);
  }

  const prefix = className(object);
  const open = prefix ? `${prefix} {` : "{";
  if (folded) return `${open}…}`;
  const keys = Reflect.ownKeys(object).filter(
    (k) => Object.getOwnPropertyDescriptor(object, k)?.enumerable,
  );
  const entries = limited(keys, (k) => {
    const descriptor = Object.getOwnPropertyDescriptor(object, k);
    const shown = descriptor && "value" in descriptor ? nested(descriptor.value) : "(...)";
    return `${key(k)}: ${shown}`;
  });
  return wrap(open, entries, "}", indent);
}

/** An array's entries, a run of holes as one. */
function arrayEntries(array: readonly unknown[]): ((show: (v: unknown) => string) => string)[] {
  const entries: ((show: (v: unknown) => string) => string)[] = [];
  for (let i = 0; i < array.length; i++) {
    if (i in array) {
      const item = array[i];
      entries.push((show) => show(item));
      continue;
    }
    let holes = 1;
    while (i + 1 < array.length && !(i + 1 in array)) {
      holes++;
      i++;
    }
    entries.push(() => `empty × ${holes}`);
  }
  return entries;
}

function firstLine(error: Error): string {
  let name = "Error";
  let message = "";
  try {
    name = String(error.name);
    message = String(error.message);
  } catch {
    // An error whose name or message throws still shows as an error.
  }
  return message ? `${name}: ${message}` : name;
}

const v8Frame = /^\s*at (?:(.+?) \()?.*?cell-(\d+)\.js:(\d+):(\d+)\)?$/;
const firefoxFrame = /^(.*?)@.*?cell-(\d+)\.js:(\d+):(\d+)$/;

/** The frames of an error's stack that are the reader's code, as `at f (cell 3:2:11)`. */
function frames(error: Error, place: Place): string[] {
  let stack = "";
  try {
    stack = String(error.stack ?? "");
  } catch {
    return [];
  }
  return stack.split("\n").flatMap((line) => {
    const found = v8Frame.exec(line) ?? firefoxFrame.exec(line);
    if (!found) return [];
    const [, name, cell, row, col] = found;
    const placed = place(Number(cell), Number(row), Number(col));
    if (!placed) return [];
    const where = `cell ${cell}:${placed[0]}:${placed[1]}`;
    const fn = name?.trim();
    return [`    at ${fn && fn !== "eval" && fn !== "<anonymous>" ? `${fn} (${where})` : where}`];
  });
}

/** value as devtools shows it on its own: strings as they are, arrays with their length, errors with the reader's frames. */
export function formatValue(value: unknown, place: Place = unmoved): string {
  if (typeof value === "string") return value;
  if (typeof value === "function") {
    const source = Function.prototype.toString.call(value);
    return source.startsWith("function") ? `ƒ ${source.slice(8).trimStart()}` : source;
  }
  if (typeof value === "object" && value !== null) {
    if (isError(value)) return [firstLine(value), ...frames(value, place)].join("\n");
    if (Array.isArray(value)) return `(${value.length}) ${inspect(value, 0, [], place)}`;
  }
  return inspect(value, 0, [], place);
}

/** A block's completion value, as devtools shows one: strings quoted, the rest as on their own. */
export function formatResult(value: unknown, place: Place = unmoved): string {
  return typeof value === "string" ? quote(value) : formatValue(value, place);
}

/** What console.log prints for args: a first string's %s, %d, %i, %f, %o, %O, %c and %% filled in. */
export function formatLog(args: readonly unknown[], place: Place = unmoved): string {
  const rest = [...args];
  const parts: string[] = [];
  const first = rest[0];
  if (typeof first === "string" && first.includes("%")) {
    rest.shift();
    parts.push(
      first.replace(/%([sdifoOc%])/g, (spec, letter: string) => {
        if (letter === "%") return "%";
        if (rest.length === 0) return spec;
        const arg = rest.shift();
        switch (letter) {
          case "s":
            return typeof arg === "object" && arg !== null ? formatValue(arg, place) : String(arg);
          case "d":
          case "i":
            return typeof arg === "bigint" ? `${arg}n` : String(Number.parseInt(String(arg), 10));
          case "f":
            return String(Number.parseFloat(String(arg)));
          case "c":
            return "";
          default:
            return inspect(arg, 0, [], place);
        }
      }),
    );
  }
  return [...parts, ...rest.map((arg) => formatValue(arg, place))].join(" ");
}

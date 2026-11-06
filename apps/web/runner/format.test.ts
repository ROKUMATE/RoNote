import { expect, test } from "vitest";
import { formatLog, formatResult, formatValue } from "./format";

class Point {
  constructor(
    public x: number,
    public y: number,
  ) {}
}

test("a value on its own shows as devtools shows it", () => {
  expect(formatValue("plain text")).toBe("plain text");
  expect(formatValue(42)).toBe("42");
  expect(formatValue(-0)).toBe("-0");
  expect(formatValue(10n)).toBe("10n");
  expect(formatValue(Symbol("tag"))).toBe("Symbol(tag)");
  expect(formatValue(null)).toBe("null");
  expect(formatValue(undefined)).toBe("undefined");
  expect(formatValue([1, "two", [3]])).toBe("(3) [1, 'two', [3]]");
  expect(formatValue({ a: "hi", "b-c": 2, [Symbol("s")]: 3 })).toBe(
    "{a: 'hi', 'b-c': 2, [Symbol(s)]: 3}",
  );
  expect(formatValue(new Point(1, 2))).toBe("Point {x: 1, y: 2}");
  expect(formatValue(/a+/g)).toBe("/a+/g");
  expect(formatValue(new Date(Number.NaN))).toBe("Invalid Date");
  expect(formatValue(Promise.resolve(1))).toBe("Promise {…}");
});

test("a completion value quotes a string, as devtools does", () => {
  expect(formatResult("Hello")).toBe("'Hello'");
  expect(formatResult([1])).toBe("(1) [1]");
});

test("nesting folds past two levels, arrays to their length", () => {
  expect(formatValue({ a: { b: { c: { d: 1 } } } })).toBe("{a: {b: {…}}}");
  expect(formatValue({ a: { b: [1, 2] } })).toBe("{a: {b: Array(2)}}");
  expect(formatValue({ a: { b: new Point(1, 2) } })).toBe("{a: {b: Point {…}}}");
  expect(formatValue({ a: { b: new Map([[1, 2]]) } })).toBe("{a: {b: Map(1)}}");
});

test("Maps and Sets show their size and entries", () => {
  expect(
    formatValue(
      new Map<unknown, unknown>([
        ["a", 1],
        ["b", { x: 1 }],
      ]),
    ),
  ).toBe("Map(2) {'a' => 1, 'b' => {x: 1}}");
  expect(formatValue(new Set([1, "two"]))).toBe("Set(2) {1, 'two'}");
  expect(formatValue(new Map())).toBe("Map(0) {}");
  expect(formatValue(new WeakMap())).toBe("WeakMap {}");
});

test("a cycle shows as [Circular], however it closes", () => {
  const node: Record<string, unknown> = { name: "node" };
  node.self = node;
  expect(formatValue(node)).toBe("{name: 'node', self: [Circular]}");
  const list: unknown[] = [1];
  list.push(list);
  expect(formatValue(list)).toBe("(2) [1, [Circular]]");
  const map = new Map<string, unknown>();
  map.set("me", map);
  expect(formatValue(map)).toBe("Map(1) {'me' => [Circular]}");
  // The same object twice, side by side, is no cycle.
  const shared = { n: 1 };
  expect(formatValue([shared, shared])).toBe("(2) [{n: 1}, {n: 1}]");
});

test("getters aren't run, and holes are counted", () => {
  const tricky = {
    get boom(): never {
      throw new Error("ran");
    },
  };
  expect(formatValue(tricky)).toBe("{boom: (...)}");
  // biome-ignore lint/suspicious/noSparseArray: the holes are the point.
  expect(formatValue([1, , , 4])).toBe("(4) [1, empty × 2, 4]");
});

test("what is too wide breaks into a line an entry, and long lists are cut", () => {
  const wide = { first: "a long enough value", second: "another long value", third: [1, 2, 3] };
  expect(formatValue(wide)).toBe(
    "{\n  first: 'a long enough value',\n  second: 'another long value',\n  third: [1, 2, 3]\n}",
  );
  const many = Array.from({ length: 150 }, (_, i) => i);
  expect(formatValue(many)).toMatch(/^\(150\) \[\n {2}0,\n[\s\S]*\n {2}99,\n {2}…50 more\n\]$/);
});

test("functions show their source alone, their name inside something", () => {
  function add(a: number, b: number) {
    return a + b;
  }
  expect(formatValue(add)).toMatch(/^ƒ add\(a, b\) \{/);
  expect(formatValue(() => 1)).toBe("() => 1");
  expect(formatValue({ add, Point })).toBe("{add: ƒ add(), Point: class Point}");
});

test("an error shows its name, message and the reader's frames only", () => {
  const error = new TypeError("x is not a function");
  error.stack = [
    "TypeError: x is not a function",
    "    at f (cell-2.js:3:5)",
    "    at cell-2.js:1:40",
    "    at run (http://localhost:5174/javascript.worker.ts:120:9)",
    "    at eval (cell-1.js:2:1)",
  ].join("\n");
  expect(formatValue(error)).toBe(
    "TypeError: x is not a function\n    at f (cell 2:3:5)\n    at cell 2:1:40\n    at cell 1:2:1",
  );
  // A position maps back through the cell's inserted text, and one in the
  // wrapper around it is left out.
  const moved = formatValue(error, (cell, line, column) =>
    line === 1 ? undefined : [line, cell === 2 ? column - 1 : column],
  );
  expect(moved).toBe("TypeError: x is not a function\n    at f (cell 2:3:4)\n    at cell 1:2:1");
  expect(formatValue({ cause: new RangeError("too far") })).toBe("{cause: RangeError: too far}");
  const firefox = new Error("boom");
  firefox.stack = "f@cell-3.js:4:2\n@cell-3.js:7:1\n";
  expect(formatValue(firefox)).toBe("Error: boom\n    at f (cell 3:4:2)\n    at cell 3:7:1");
});

test("console.log fills in a first string's specifiers, then shows the rest", () => {
  expect(formatLog(["%s is %d years and %i days", "Ada", 36.6, "12x"])).toBe(
    "Ada is 36 years and 12 days",
  );
  expect(formatLog(["%f and %o", "2.5kg", "text"])).toBe("2.5 and 'text'");
  expect(formatLog(["%cstyled", "color: red"])).toBe("styled");
  expect(formatLog(["100%%"])).toBe("100%");
  expect(formatLog(["%s and %s", "one"])).toBe("one and %s");
  expect(formatLog(["total", { b: 1 }, [1], "end"])).toBe("total {b: 1} (1) [1] end");
  expect(formatLog([{ a: 1 }, "x"])).toBe("{a: 1} x");
  expect(formatLog([])).toBe("");
});

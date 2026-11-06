import { afterEach, expect, test } from "vitest";
import { declare, place, prepare } from "./evaluate";

// Called by another name, eval is indirect, as in the JavaScript worker.
// biome-ignore lint/security/noGlobalEval: the test runs code as the runner's JavaScript worker does.
const indirect = globalThis.eval;

const declared: string[] = [];
let cells = 0;

/** Runs code as the JavaScript worker would; its completion value comes back in an array, as there. */
async function run(code: string): Promise<[unknown] | undefined> {
  cells += 1;
  const prepared = prepare(code, `cell-${cells}.js`);
  declared.push(...prepared.names);
  declare(prepared.names);
  return await indirect(prepared.source);
}

async function evaluate(code: string): Promise<unknown> {
  return (await run(code))?.[0];
}

afterEach(() => {
  for (const name of declared.splice(0)) Reflect.deleteProperty(globalThis, name);
});

test("blocks share what they declare, through the global object", async () => {
  await evaluate(
    "const greeting = 'Hello';\nlet count = 1, spare;\nvar { a, b: [c] } = { a: 2, b: [3] };",
  );
  await evaluate(
    "function twice(n) { return n * 2; }\nclass Box { constructor(v) { this.v = v; } }",
  );
  // biome-ignore lint/suspicious/noTemplateCurlyInString: the reader's JavaScript, as text.
  expect(await evaluate("`${greeting} ${count} ${spare} ${a} ${c}`")).toBe("Hello 1 undefined 2 3");
  expect(await evaluate("new Box(twice(21)).v")).toBe(42);
});

test("a block runs again without 'already declared'", async () => {
  const block = "const tally = (globalThis.tally ?? 0) + 1;\ntally";
  expect(await evaluate(block)).toBe(1);
  expect(await evaluate(block)).toBe(2);
});

test("a block may await at its top level", async () => {
  expect(await evaluate("const v = await Promise.resolve(41);\nv + 1")).toBe(42);
});

test("only a trailing expression gives a value, and a promise comes back as itself", async () => {
  expect(await evaluate("const quiet = 1;")).toBeUndefined();
  expect(await evaluate("if (true) { 1 }")).toBeUndefined();
  const [pending] = (await run("new Promise(() => {})")) ?? [];
  expect(pending).toBeInstanceOf(Promise);
});

test("a name the page's global object owns can still be declared", async () => {
  Object.defineProperty(globalThis, "fixedName", {
    get: () => "the worker's",
    configurable: true,
    enumerable: false,
  });
  declared.push("fixedName");
  expect(await evaluate("const fixedName = 'the reader\\'s';\nfixedName")).toBe("the reader's");
});

test("a block's lines stay where they were, and columns map back", async () => {
  const code = "const a = 1;\nfunction f() {\n  throw new Error('here');\n}\nf()";
  const prepared = prepare(code, "cell-9.js");
  declare(prepared.names);
  declared.push(...prepared.names);
  const error: Error = await indirect(prepared.source).catch((e: Error) => e);
  expect(error.stack).toMatch(/cell-9\.js:3:9/);
  // Columns from 1, as stacks give them. Line 1 carries the wrapper and the
  // prelude before the block's own text.
  const head = prepared.inserts[0]?.length ?? 0;
  expect(place(prepared.inserts, 1, head + 7)).toEqual([1, 7]);
  expect(place(prepared.inserts, 1, 2)).toEqual([1, 1]);
  // The completion's `return [` before f() on line 5.
  expect(place(prepared.inserts, 5, 9)).toEqual([5, 1]);
  expect(place(prepared.inserts, 5, 3)).toEqual([5, 1]);
  expect(place(prepared.inserts, 3, 9)).toEqual([3, 9]);
  expect(prepared.lines).toBe(5);
});

test("code that isn't a script throws a SyntaxError saying where", () => {
  expect(() => prepare("const = 1", "cell-1.js")).toThrow(/Unexpected token \(1:6\)/);
  expect(() => prepare('import x from "y"', "cell-1.js")).toThrow(SyntaxError);
});

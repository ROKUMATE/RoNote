import { type Pattern, parse } from "acorn";

/*
 * How the JavaScript worker runs a block (docs/adr/0008-in-browser-code-execution.md).
 * A block runs as a script with top-level await, wrapped in an async
 * function, so its top-level declarations become assignments to names on
 * the global object: later blocks see them, and a block runs again without
 * "already declared", as in devtools' console. A trailing expression's value
 * is returned, as the completion value. The block's text itself never
 * changes; only what runs does, and a position in it maps back.
 */

/** Text inserted where the block had none, at a line (from 1) and column (from 0) of the block. */
export type Insert = { line: number; column: number; length: number };

export type Prepared = {
  /** What to evaluate: it gives a promise of [the completion value], or of nothing. */
  source: string;
  /** The names the block declares at its top level, to put on the global object first. */
  names: string[];
  inserts: Insert[];
  /** How many lines the block has: past them, a position is the wrapper's. */
  lines: number;
};

/** The identifiers a declaration's pattern binds. */
function bound(pattern: Pattern): string[] {
  switch (pattern.type) {
    case "Identifier":
      return [pattern.name];
    case "ObjectPattern":
      return pattern.properties.flatMap((p) => bound(p.type === "RestElement" ? p : p.value));
    case "ArrayPattern":
      return pattern.elements.flatMap((e) => (e ? bound(e) : []));
    case "RestElement":
      return bound(pattern.argument);
    case "AssignmentPattern":
      return bound(pattern.left);
    case "MemberExpression":
      return [];
  }
}

/**
 * code ready to evaluate as file, its sourceURL. Throws acorn's SyntaxError,
 * whose message gives the line and column, when code isn't a script.
 */
export function prepare(code: string, file: string): Prepared {
  const program = parse(code, {
    ecmaVersion: "latest",
    sourceType: "script",
    allowAwaitOutsideFunction: true,
  });
  const edits: { at: number; remove: number; text: string }[] = [];
  const names: string[] = [];
  let prelude = "";

  const last = program.body.at(-1);
  for (const statement of program.body) {
    switch (statement.type) {
      case "VariableDeclaration": {
        if (statement.kind !== "var" && statement.kind !== "let" && statement.kind !== "const")
          break;
        // `const ` becomes `(     `: the same width, so columns hold.
        const keyword = statement.kind.length;
        edits.push({ at: statement.start, remove: keyword, text: `(${" ".repeat(keyword - 1)}` });
        for (const declarator of statement.declarations) {
          names.push(...bound(declarator.id));
          if (!declarator.init) edits.push({ at: declarator.end, remove: 0, text: " = undefined" });
        }
        const end = statement.declarations.at(-1)?.end ?? statement.end;
        edits.push({ at: end, remove: 0, text: ")" });
        break;
      }
      case "FunctionDeclaration":
        // Hoisted inside the wrapper, so the prelude can hand it to the global object.
        names.push(statement.id.name);
        prelude += `globalThis.${statement.id.name} = ${statement.id.name}; `;
        break;
      case "ClassDeclaration":
        names.push(statement.id.name);
        edits.push({ at: statement.start, remove: 0, text: `${statement.id.name} = ` });
        edits.push({ at: statement.end, remove: 0, text: ";" });
        break;
      case "ExpressionStatement":
        // In an array, so a promise comes back as itself rather than awaited.
        if (statement === last && !statement.directive) {
          edits.push({ at: statement.expression.start, remove: 0, text: "return [" });
          edits.push({ at: statement.expression.end, remove: 0, text: "]" });
        }
        break;
    }
  }

  let body = "";
  let from = 0;
  const sorted = edits
    .map((edit, order) => ({ ...edit, order }))
    .sort((a, b) => a.at - b.at || a.order - b.order);
  for (const edit of sorted) {
    body += code.slice(from, edit.at) + edit.text;
    from = edit.at + edit.remove;
  }
  body += code.slice(from);

  const head = `(async () => {${prelude}`;
  const lines = lineStarts(code);
  const inserts: Insert[] = [
    { line: 1, column: 0, length: head.length },
    ...sorted
      .filter((edit) => edit.remove === 0)
      .map((edit) => ({ ...position(lines, edit.at), length: edit.text.length })),
  ];
  return {
    source: `${head}${body}\n})()\n//# sourceURL=${file}`,
    names,
    inserts,
    lines: lines.length,
  };
}

function lineStarts(code: string): number[] {
  const starts = [0];
  for (let i = 0; i < code.length; i++) if (code[i] === "\n") starts.push(i + 1);
  return starts;
}

function position(starts: readonly number[], at: number): { line: number; column: number } {
  let line = 0;
  while (line + 1 < starts.length && (starts[line + 1] ?? 0) <= at) line++;
  return { line: line + 1, column: at - (starts[line] ?? 0) };
}

/**
 * A position in what ran, as stacks give one (line and column from 1), as
 * the same place in the block.
 */
export function place(inserts: readonly Insert[], line: number, column: number): [number, number] {
  const at = column - 1;
  let shift = 0;
  for (const insert of inserts) {
    if (insert.line !== line) continue;
    const start = insert.column + shift;
    if (at < start) break;
    // Inside inserted text: the place it was inserted at.
    if (at < start + insert.length) return [line, insert.column + 1];
    shift += insert.length;
  }
  return [line, at - shift + 1];
}

/**
 * Makes each name a plain, writable property of the global object, so a
 * block's assignments land there even where the worker has a read-only
 * name of its own, such as `name`; a value from an earlier block stays.
 */
export function declare(names: readonly string[]) {
  for (const name of names) {
    const own = Object.getOwnPropertyDescriptor(globalThis, name);
    if (own && "value" in own && own.writable) continue;
    if (own && !own.configurable) continue;
    Object.defineProperty(globalThis, name, {
      value: own && "value" in own ? own.value : undefined,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
}

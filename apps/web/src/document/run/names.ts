import { predefined } from "./python";

/*
 * What names a Python block binds and uses, read from its text: enough to
 * tell which blocks a block needs, not a parser. Binding is over-counted on
 * purpose (keyword arguments count, for one), so a name is free only when
 * the block surely doesn't bind it.
 */

/**
 * source with the insides of strings and all comments blanked to spaces,
 * line breaks kept, and whether a string is left open.
 */
export function codeOnly(source: string): { code: string; open: boolean } {
  let code = "";
  let quote = "";
  for (let i = 0; i < source.length; i++) {
    const c = source[i] ?? "";
    if (quote) {
      if (c === "\\" && i + 1 < source.length) {
        code += source[i + 1] === "\n" ? " \n" : "  ";
        i++;
      } else if (source.startsWith(quote, i)) {
        code += quote;
        i += quote.length - 1;
        quote = "";
      } else if (c === "\n" && quote.length === 1) {
        return { code: code + source.slice(i), open: true };
      } else {
        code += c === "\n" ? "\n" : " ";
      }
    } else if (c === "#") {
      const end = source.indexOf("\n", i);
      const stop = end < 0 ? source.length : end;
      code += " ".repeat(stop - i);
      i = stop - 1;
    } else if (c === '"' || c === "'") {
      // A prefix such as f or rb belongs to the string, not to the code.
      const prefix = /(?:^|[^\w])([rRbBuUfFtT]{1,2})$/.exec(code)?.[1];
      if (prefix) code = code.slice(0, -prefix.length) + " ".repeat(prefix.length);
      quote = source.startsWith(c.repeat(3), i) ? c.repeat(3) : c;
      code += quote;
      i += quote.length - 1;
    } else {
      code += c;
    }
  }
  return { code, open: quote !== "" };
}

const identifier = /[A-Za-z_][A-Za-z0-9_]*/g;

/** The identifiers in text, attributes (after a dot) and the letters of numbers left out. */
function identifiers(text: string): string[] {
  return [...text.matchAll(identifier)]
    .filter((m) => {
      let i = m.index - 1;
      while (i >= 0 && (text[i] === " " || text[i] === "\t")) i--;
      return !(text[i] === "." || /\d/.test(text[m.index - 1] ?? ""));
    })
    .map((m) => m[0]);
}

/** The first identifier of each top-level, comma-separated part of text. */
function firstOfEach(text: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let part = "";
  for (const c of text) {
    if ("([{".includes(c)) depth++;
    if (")]}".includes(c)) depth--;
    if (c === "," && depth === 0) {
      parts.push(part);
      part = "";
    } else {
      part += c;
    }
  }
  parts.push(part);
  return parts.flatMap((p) => identifiers(p).slice(0, 1));
}

/** The parameter list of the def starting at index start of code, up to its closing bracket. */
function parameters(code: string, open: number): string {
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    if (code[i] === "(") depth++;
    if (code[i] === ")" && --depth === 0) return code.slice(open + 1, i);
  }
  return code.slice(open + 1);
}

/**
 * The names a case pattern captures: bare names, not a class pattern's class,
 * a keyword pattern's attribute, a dotted constant or the wildcard.
 */
function captures(pattern: string): string[] {
  return [...pattern.matchAll(identifier)]
    .filter((m) => {
      const before = pattern.slice(0, m.index).trimEnd();
      const after = pattern.slice(m.index + m[0].length).trimStart();
      return !before.endsWith(".") && !/^(?:[(.]|=(?!=))/.test(after) && !/\d$/.test(before);
    })
    .map((m) => m[0])
    .filter((name) => name !== "_" && !predefined.has(name));
}

export type Names = {
  /** The names the block binds: definitions, imports, assignments, parameters, loop targets. */
  binds: Set<string>;
  /**
   * The names it binds for the code after it: those bound outside any def or
   * class (and declared global), not parameters, locals or keyword arguments.
   */
  exports: Set<string>;
  /** The names it uses without binding them, builtins and keywords left out. */
  free: Set<string>;
  /** Whether it imports everything from a module, so any name may be bound. */
  star: boolean;
};

export function names(source: string): Names {
  const { code } = codeOnly(source);
  const binds = new Set<string>();
  const exports = new Set<string>();
  const bind = (list: Iterable<string>, exported: boolean) => {
    for (const name of list) {
      binds.add(name);
      if (exported) exports.add(name);
    }
  };
  let star = false;

  /** What the statement starting on line binds; at module level, for later blocks too. */
  const statement = (line: string, module: boolean) => {
    const from = /^\s*from\s+[\w.]+\s+import\s+(.+)$/.exec(line);
    if (from?.[1]) {
      if (from[1].trim().startsWith("*")) star = true;
      for (const part of from[1].replace(/[()]/g, "").split(",")) {
        bind(identifiers(part).slice(-1), module);
      }
      return;
    }
    const plain = /^\s*import\s+(.+)$/.exec(line);
    if (plain?.[1]) {
      for (const part of plain[1].split(",")) {
        const alias = /\bas\s+(\w+)/.exec(part)?.[1];
        bind(alias ? [alias] : identifiers(part).slice(0, 1), module);
      }
      return;
    }
    const definition = /^\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_]\w*)/.exec(line)?.[1];
    if (definition) bind([definition], module);
    const loop = /^\s*(?:async\s+)?for\s+(.+?)\s+in\b/.exec(line)?.[1];
    if (loop) bind(identifiers(loop), module);
    const pattern = /^\s*case\s+(.+?)(?:\s+if\b.*)?:\s*$/.exec(line)?.[1];
    if (pattern) bind(captures(pattern), module);
    for (const m of line.matchAll(/\bas\s+([A-Za-z_]\w*)/g)) bind(m.slice(1, 2), module);
    for (const m of line.matchAll(/([A-Za-z_]\w*)\s*:=/g)) bind(m.slice(1, 2), module);
    // Assignment targets, plain, annotated, augmented or unpacked; one with a
    // bracket left open is a call's keyword argument.
    const assign = /^\s*([^=]*?[\w)\]])\s*(?::[^=]*)?(?:[-+*/%&|^@]|\/\/|\*\*|>>|<<)?=(?!=)/.exec(
      line,
    );
    const target = assign?.[1] ?? "";
    const open = (target.match(/[([{]/g)?.length ?? 0) > (target.match(/[)\]}]/g)?.length ?? 0);
    if (target && !open && !/\b(if|elif|while|return|assert|lambda)\b/.test(target)) {
      bind(identifiers(target.replace(/\[[^\]]*\]/g, "")), module);
    }
  };

  // Each statement's first line, with whether it is outside every def and
  // class body: bodies are what is indented past their def or class line.
  // Lines inside brackets continue a statement and change no scope.
  const scopes: number[] = [];
  let depth = 0;
  for (const line of code.split("\n")) {
    if (depth === 0 && line.trim() !== "") {
      const indent = line.length - line.trimStart().length;
      while (scopes.length > 0 && indent <= (scopes.at(-1) ?? 0)) scopes.pop();
      statement(line, scopes.length === 0);
      if (/^\s*(?:async\s+)?(?:def|class)\b/.test(line)) scopes.push(indent);
    }
    for (const c of line) {
      if ("([{".includes(c)) depth++;
      else if (")]}".includes(c)) depth = Math.max(0, depth - 1);
    }
  }

  // Bound inside the block, but never for the code after it.
  for (const m of code.matchAll(/\b(?:def|class)\s+[A-Za-z_]\w*\s*\(/g)) {
    bind(firstOfEach(parameters(code, m.index + m[0].length - 1)), false);
  }
  for (const m of code.matchAll(/\blambda\b([^:]*):/g)) bind(firstOfEach(m[1] ?? ""), false);
  // Comprehension variables, and keyword arguments, which name no variable at all.
  for (const m of code.matchAll(/\bfor\s+(.+?)\s+in\b/g)) bind(identifiers(m[1] ?? ""), false);
  for (const m of code.matchAll(/([A-Za-z_]\w*)\s*=(?!=)/g)) bind(m.slice(1, 2), false);
  for (const m of code.matchAll(/\bnonlocal\s+([\w,\s]+)/g)) bind(identifiers(m[1] ?? ""), false);
  for (const m of code.matchAll(/\bglobal\s+([\w,\s]+)/g)) bind(identifiers(m[1] ?? ""), true);

  // An import line's module names are bindings or paths, never uses.
  const uses = code
    .split("\n")
    .filter((l) => !/^\s*(import|from\s+[\w.]+\s+import)\b/.test(l))
    .join("\n");
  const free = new Set(
    identifiers(uses).filter((n) => !binds.has(n) && !predefined.has(n) && !/^__\w+__$/.test(n)),
  );
  return { binds, exports, free, star };
}

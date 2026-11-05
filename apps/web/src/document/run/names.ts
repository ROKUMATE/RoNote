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

export type Names = {
  /** The names the block binds: definitions, imports, assignments, parameters, loop targets. */
  binds: Set<string>;
  /** The names it uses without binding them, builtins and keywords left out. */
  free: Set<string>;
  /** Whether it imports everything from a module, so any name may be bound. */
  star: boolean;
};

export function names(source: string): Names {
  const { code } = codeOnly(source);
  const binds = new Set<string>();
  const bind = (list: Iterable<string>) => {
    for (const name of list) binds.add(name);
  };
  let star = false;

  for (const m of code.matchAll(/\b(?:def|class)\s+([A-Za-z_]\w*)\s*(\()?/g)) {
    if (m[1]) binds.add(m[1]);
    if (m[2] && m.index !== undefined) {
      bind(firstOfEach(parameters(code, m.index + m[0].length - 1)));
    }
  }
  for (const m of code.matchAll(/\blambda\b([^:]*):/g)) bind(firstOfEach(m[1] ?? ""));
  for (const line of code.split("\n")) {
    const from = /^\s*from\s+[\w.]+\s+import\s+(.+)$/.exec(line);
    if (from?.[1]) {
      if (from[1].trim().startsWith("*")) star = true;
      for (const part of from[1].replace(/[()]/g, "").split(",")) {
        const words = identifiers(part);
        const name = words.at(-1);
        if (name) binds.add(name);
      }
      continue;
    }
    const plain = /^\s*import\s+(.+)$/.exec(line);
    if (plain?.[1]) {
      for (const part of plain[1].split(",")) {
        const alias = /\bas\s+(\w+)/.exec(part)?.[1];
        const name = alias ?? identifiers(part)[0];
        if (name) binds.add(name);
      }
      continue;
    }
    // Assignment targets, plain, annotated, augmented or unpacked.
    const assign = /^\s*([^=]*?[\w)\]])\s*(?::[^=]*)?(?:[-+*/%&|^@]|\/\/|\*\*|>>|<<)?=(?!=)/.exec(
      line,
    );
    if (assign?.[1] && !/\b(if|elif|while|return|assert|lambda)\b/.test(assign[1])) {
      bind(identifiers(assign[1].replace(/\[[^\]]*\]/g, "")));
    }
  }
  for (const m of code.matchAll(/\bfor\s+(.+?)\s+in\b/g)) bind(identifiers(m[1] ?? ""));
  for (const m of code.matchAll(/\bas\s+([A-Za-z_]\w*)/g)) if (m[1]) binds.add(m[1]);
  for (const m of code.matchAll(/\b(?:global|nonlocal)\s+([\w,\s]+)/g))
    bind(identifiers(m[1] ?? ""));
  // Keyword arguments and walrus targets.
  for (const m of code.matchAll(/([A-Za-z_]\w*)\s*:?=(?!=)/g)) if (m[1]) binds.add(m[1]);

  // An import line's module names are bindings or paths, never uses.
  const uses = code
    .split("\n")
    .filter((l) => !/^\s*(import|from\s+[\w.]+\s+import)\b/.test(l))
    .join("\n");
  const free = new Set(
    identifiers(uses).filter((n) => !binds.has(n) && !predefined.has(n) && !/^__\w+__$/.test(n)),
  );
  return { binds, free, star };
}

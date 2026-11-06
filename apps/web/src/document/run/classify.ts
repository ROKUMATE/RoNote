import type { IrBlock } from "../../api/schema";
import type { Runtime } from "../../runner/protocol";
import { eachBlock } from "../walk";
import { jsCode } from "./javascript";
import { codeOnly, type Names, names } from "./names";
import { importable } from "./python";

/*
 * Whether a code block can run in the browser, and if not, why
 * (docs/adr/0008-in-browser-code-execution.md). Derived from the blocks'
 * languages and text every time, never stored.
 */

/** What kind of code a block holds, as far as running it goes. */
export type RunClass =
  | "browser_python"
  | "python_doctest"
  | "browser_js"
  | "needs_packages"
  | "needs_network"
  | "needs_secret"
  | "needs_input"
  | "needs_dom"
  | "shell"
  | "fragment"
  | "unsupported";

/** One plain sentence per class, as the reader sees it. */
export const reasons: Record<RunClass, string> = {
  browser_python: "Python that runs here, in your browser.",
  python_doctest: "A Python session: it runs line by line, here in your browser.",
  browser_js: "JavaScript that runs here, in your browser.",
  needs_packages: "It needs packages that aren't available in the browser.",
  needs_network: "It reaches the network, which code here can't.",
  needs_secret: "It needs an API key or another secret.",
  needs_input: "It asks for input as it runs.",
  needs_dom: "It needs a window or a display.",
  shell: "It runs commands on your computer: try it in a terminal.",
  fragment: "It's part of a larger program, so it may not run on its own.",
  unsupported: "Only Python and JavaScript run here for now.",
};

/** Whether a class shows Run, "Run anyway", or only its reason. */
const runOf: Record<RunClass, Runnability["run"]> = {
  browser_python: "run",
  python_doctest: "run",
  browser_js: "run",
  needs_packages: "anyway",
  fragment: "anyway",
  needs_network: "none",
  needs_secret: "none",
  needs_input: "none",
  needs_dom: "none",
  shell: "none",
  unsupported: "none",
};

export type Runnability = {
  class: RunClass;
  reason: string;
  run: "run" | "anyway" | "none";
  /** The language's runtime, which Run or Run anyway runs it in; none for shell or unsupported code. */
  runtime?: Runtime;
};

function classed(c: RunClass, runtime?: Runtime): Runnability {
  const found = { class: c, reason: reasons[c], run: runOf[c] };
  return runtime ? { ...found, runtime } : found;
}

const fragment = classed("fragment", "python");

const python = new Set(["python", "py", "python3", "py3", "ipython", "pycon", "python-repl"]);
const javascript = new Set(["javascript", "js", "mjs", "cjs"]);
const shells = new Set([
  "bash",
  "sh",
  "shell",
  "zsh",
  "fish",
  "console",
  "shellsession",
  "shell-session",
  "terminal",
  "powershell",
  "ps1",
  "pwsh",
  "cmd",
  "bat",
  "batch",
]);

/** Imports whose code reaches the network. */
const network = new Set([
  "requests",
  "httpx",
  "aiohttp",
  "urllib3",
  "websockets",
  "websocket",
  "socket",
  "http.client",
  "urllib.request",
  "smtplib",
  "ftplib",
  "imaplib",
  "poplib",
  "xmlrpc.client",
  "pyodide.http",
]);

/** Imports that need a window, a display or the page. */
const display = new Set(["tkinter", "turtle", "webbrowser", "curses", "pygame"]);

/** Imports that start other programs. */
const processes = new Set(["subprocess", "multiprocessing"]);

/** Order of how much a class keeps from running. */
const strictness = { run: 0, anyway: 1, none: 2 } as const;

export type CodeBlock = { id: string; code: string; lang: string | undefined };

/**
 * The code blocks in blocks, nested ones included, in document order, each in
 * the language the reader chose for it or else the page's.
 */
export function codeBlocks(
  blocks: readonly IrBlock[],
  chosen: Readonly<Record<string, string>> = {},
): CodeBlock[] {
  const found: CodeBlock[] = [];
  eachBlock(blocks, (b) => {
    if (b.kind === "code")
      found.push({ id: b.id, code: b.text ?? "", lang: chosen[b.id] ?? b.lang });
  });
  return found;
}

/** Whether code is a >>> transcript, which runs a statement at a time. */
export function isDoctest(code: string): boolean {
  return /^\s*>>> /m.test(code);
}

/**
 * Every block's class, read across the document, as a notebook's cells
 * share one session: a Python block that uses a name only blocks that can't
 * run define takes the stricter of their classes, and one that uses a name no
 * block defines is a fragment. Keyed by block id.
 */
export function classifyAll(blocks: readonly CodeBlock[]): Map<string, Runnability> {
  const classes = new Map<string, Runnability>();
  const read = new Map<string, Names>();
  for (const b of blocks) {
    const r = classify(b.code, b.lang);
    classes.set(b.id, r);
    if (r.runtime === "python") {
      read.set(b.id, names(isDoctest(b.code) ? doctestSource(b.code) : b.code));
    }
  }
  const binders = new Map<string, string[]>();
  const stars: string[] = [];
  for (const [id, n] of read) {
    for (const name of n.exports) binders.set(name, [...(binders.get(name) ?? []), id]);
    if (n.star) stars.push(id);
  }
  const strictest = (ids: readonly string[]) =>
    ids
      .map((id) => classes.get(id))
      .reduce<Runnability | undefined>(
        (a, r) => (r && (!a || strictness[r.run] > strictness[a.run]) ? r : a),
        undefined,
      );

  // Each pass only makes blocks stricter, so it settles.
  for (let changed = true; changed; ) {
    changed = false;
    for (const [id, n] of read) {
      let r = classes.get(id);
      if (!r || r.run === "none") continue;
      for (const name of n.free) {
        const others = (binders.get(name) ?? stars).filter((b) => b !== id);
        if (others.length === 0 && n.star) continue;
        if (others.some((b) => classes.get(b)?.run === "run")) continue;
        const from = others.length === 0 ? fragment : strictest(others);
        if (from && strictness[from.run] > strictness[r.run]) r = from;
      }
      if (r !== classes.get(id)) {
        classes.set(id, r);
        changed = true;
      }
    }
  }
  return classes;
}

/** A block's class, from its language (the reader's choice over the page's) and text alone. */
export function classify(code: string, lang: string | undefined): Runnability {
  const language = lang?.toLowerCase();
  if (language && shells.has(language)) return classed("shell");
  if (language && javascript.has(language)) return classifyJavaScript(code);
  if (!language || !python.has(language)) return classed("unsupported");
  return classifyPython(code);
}

function classifyPython(code: string): Runnability {
  const result = (c: RunClass) => classed(c, c === "shell" ? undefined : "python");
  const doctest = isDoctest(code);
  const source = doctest ? doctestSource(code) : code;
  const lines = codeLines(source);
  if (lines.some((l) => /^\s*(\$ |pip3? install |python3? -m |[!%]pip )/.test(l))) {
    return result("shell");
  }
  const modules = imports(lines);
  const uses = (pattern: RegExp) => lines.some((l) => pattern.test(l));

  // Reading the environment is fine; reading a secret from it is not.
  const secretName = /(KEY|SECRET|TOKEN|PASSWORD|PASSWD|CREDENTIAL|AUTH)/i;
  if (
    lines.some((l) => /\b(os\.environ|os\.getenv|getenv\()/.test(l) && secretName.test(l)) ||
    uses(/\bload_dotenv\b|\bgetpass\b/) ||
    uses(/\b[A-Z0-9_]*(API_KEY|SECRET|ACCESS_TOKEN|PASSWORD)[A-Z0-9_]*\b|\bapi_key\s*=/) ||
    modules.some((m) => m === "dotenv" || m === "getpass")
  ) {
    return result("needs_secret");
  }
  if (uses(/(^|[^\w.])input\s*\(|\bsys\.stdin\b|\bsys\.argv\b|\.parse_args\(/)) {
    return result("needs_input");
  }
  if (
    modules.some((m) => display.has(root(m))) ||
    uses(/\bjs\.(document|window)\b|from js import .*\b(document|window)\b/)
  ) {
    return result("needs_dom");
  }
  if (
    modules.some((m) => network.has(m) || network.has(root(m))) ||
    uses(/\burlopen\(|\bopen_connection\(/)
  ) {
    return result("needs_network");
  }
  if (
    modules.some((m) => processes.has(root(m))) ||
    uses(/\bos\.(system|popen|exec\w*|spawn\w*)\(/)
  ) {
    return result("shell");
  }
  if (modules.some((m) => !importable.has(root(m)))) return result("needs_packages");
  if (looksPartial(source)) return result("fragment");
  return result(doctest ? "python_doctest" : "browser_python");
}

/** Names only a page has, not a worker: its document and window, dialogs, storage. */
const pageOnly =
  /(^|[^\w$.])(document|window|alert|confirm|localStorage|sessionStorage|customElements|requestAnimationFrame|getComputedStyle|matchMedia)\b|\bnew (Image|Audio)\b/m;

/** The modules a script imports or requires, by their specifiers. */
function specifiers(source: string): string[] {
  const found: string[] = [];
  for (const match of source.matchAll(
    /^\s*import\s+(?:[^'"]*?\s+from\s+)?["']([^"']+)["']|\b(?:require|import)\s*\(\s*["']([^"']+)["']\s*\)/gm,
  )) {
    const specifier = match[1] ?? match[2];
    if (specifier) found.push(specifier);
  }
  return found;
}

function classifyJavaScript(source: string): Runnability {
  const result = (c: RunClass) => classed(c, c === "shell" ? undefined : "javascript");
  if (/^\s*(\$ |(npm|npx|pnpm|yarn|node|deno|bun) )/m.test(source)) return result("shell");
  const { code, uncommented, open } = jsCode(source);
  const lines = code.split("\n").filter((l) => l.trim() !== "");

  // Reading the environment is fine; reading a secret from it is not.
  const secretName = /(KEY|SECRET|TOKEN|PASSWORD|PASSWD|CREDENTIAL|AUTH)/i;
  if (
    lines.some((l) => /\bprocess\.env\b/.test(l) && secretName.test(l)) ||
    /\b[A-Z0-9_]*(API_KEY|SECRET|ACCESS_TOKEN|PASSWORD)[A-Z0-9_]*\b|\bapi_?[kK]ey\s*[:=]/.test(code)
  ) {
    return result("needs_secret");
  }
  if (/(^|[^\w$.])prompt\s*\(|\bprocess\.(stdin|argv)\b|\breadline\b/m.test(code)) {
    return result("needs_input");
  }
  if (pageOnly.test(code)) return result("needs_dom");
  if (
    /(^|[^\w$.])fetch\s*\(|\b(XMLHttpRequest|WebSocket|EventSource|axios)\b|\.sendBeacon\s*\(/m.test(
      code,
    )
  ) {
    return result("needs_network");
  }
  const modules = specifiers(uncommented);
  if (modules.some((m) => !/^[./]/.test(m))) return result("needs_packages");
  if (
    open ||
    modules.length > 0 ||
    /^\s*export\b|\bmodule\.exports\b|^\s*exports\./m.test(code) ||
    /^\s/.test(lines[0] ?? "") ||
    /^(?:[}\])]|\.(?!\.\.)|(?:else|catch|finally|case|default|return|break|continue)\b)/.test(
      lines[0] ?? "",
    ) ||
    lines.some((l) => /^\s*(\.\.\.|…)\s*$/.test(l))
  ) {
    return result("fragment");
  }
  return result("browser_js");
}

/** A doctest transcript's source: its >>> and ... lines, without the prompts. */
export function doctestSource(code: string): string {
  return code
    .split("\n")
    .flatMap((line) => {
      const m = /^\s*(>>>|\.\.\.)( |$)(.*)$/.exec(line);
      return m ? [m[3] ?? ""] : [];
    })
    .join("\n");
}

/** The lines that hold code, comments and blank lines left out. */
function codeLines(source: string): string[] {
  return source.split("\n").filter((l) => l.trim() !== "" && !/^\s*#/.test(l));
}

function root(module: string): string {
  return module.split(".")[0] ?? module;
}

/** The modules a block imports, by their dotted names; relative imports left out. */
function imports(lines: readonly string[]): string[] {
  const found: string[] = [];
  for (const line of lines) {
    const from = /^\s*from\s+([\w.]+)\s+import\b/.exec(line);
    if (from?.[1] && !from[1].startsWith(".")) {
      found.push(from[1]);
      continue;
    }
    const plain = /^\s*import\s+(.+)$/.exec(line);
    if (plain?.[1]) {
      for (const part of plain[1].split(",")) {
        const name = part.trim().split(/\s+/)[0];
        if (name && /^[\w.]+$/.test(name)) found.push(name);
      }
    }
  }
  return found;
}

/**
 * Whether code looks cut from something larger: it starts indented or with a
 * clause of a statement that starts elsewhere, ends on a block opener with no
 * body, leaves a bracket or string open, or holds a <placeholder> where a
 * value goes.
 */
function looksPartial(source: string): boolean {
  const lines = codeLines(source);
  const first = lines[0];
  const last = lines.at(-1)?.trimEnd();
  if (!first || /^\s/.test(first) || last?.endsWith(":")) return true;
  const { code, open } = codeOnly(source);
  if (open) return true;
  // case is a keyword only where a clause starts, so case = 1 is code.
  const opening =
    code
      .split("\n")
      .find((line) => line.trim() !== "")
      ?.trimEnd() ?? "";
  if (
    /^(?:elif|except|return|yield|break|continue)\b|^(?:else|finally)\s*:|^case\b.*:$/.test(opening)
  )
    return true;
  let depth = 0;
  for (const c of code) {
    if ("([{".includes(c)) depth++;
    else if (")]}".includes(c) && --depth < 0) return true;
  }
  return depth !== 0 || /(?:^|[^\w)\]\s])\s*<[A-Za-z][\w-]*>/m.test(code);
}

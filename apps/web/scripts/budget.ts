import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";

/*
 * Fails if the production web bundle is over its gzip budgets (ADR 0006),
 * checked per chunk group from Vite's build manifest. The budgets live here
 * and nowhere else; 1 kB is 1000 bytes, as Vite reports sizes.
 *
 *   node scripts/budget.ts [DIST]     DIST defaults to dist
 */

const budgets = {
  /** What every page loads: the entry and its static imports. */
  initial: 150_000,
  /** The document route: the reader and editor, on top of initial. */
  document: 135_000,
  /** The syntax highlighter's core, loaded when code nears the viewport. */
  highlighter: 60_000,
  /** The highlight and note controls, loaded once the editor is up (ADR 0007). */
  annotations: 15_000,
  /** All stylesheets. */
  css: 20_000,
};

type Chunk = {
  file: string;
  imports?: string[];
  dynamicImports?: string[];
  css?: string[];
  isEntry?: boolean;
  isDynamicEntry?: boolean;
};

const dist = process.argv[2] ?? "dist";
let manifest: Record<string, Chunk>;
try {
  manifest = JSON.parse(readFileSync(`${dist}/.vite/manifest.json`, "utf8"));
} catch {
  console.error(`web-budget: no build manifest in ${dist}; run make web-build`);
  process.exit(1);
}

const gzipped = (file: string) => gzipSync(readFileSync(`${dist}/${file}`)).length;

/** key's chunk and every chunk it imports statically, minus those in seen. */
function closure(key: string, seen: Set<string>): string[] {
  if (seen.has(key)) return [];
  seen.add(key);
  return [key, ...(manifest[key]?.imports ?? []).flatMap((k) => closure(k, seen))];
}

function entry(source: string): string {
  const key = Object.keys(manifest).find((k) => k === source);
  if (!key) {
    console.error(`web-budget: ${source} is not a chunk of the build`);
    process.exit(1);
  }
  return key;
}

const seen = new Set<string>();
const groups = {
  initial: closure(entry("index.html"), seen),
  document: closure(entry("src/pages/DocumentPage.tsx"), seen),
  highlighter: closure(entry("src/document/highlighter.ts"), seen),
  annotations: closure(entry("src/document/annotations/ui.tsx"), seen),
};
const languages = Object.keys(manifest).filter(
  (k) => !seen.has(k) && manifest[k]?.file.endsWith(".js"),
);

let over = false;
function report(name: string, files: string[], budget?: number) {
  let total = 0;
  for (const file of files) {
    const size = gzipped(file);
    total += size;
    console.log(`    ${file.padEnd(48)} ${String(size).padStart(9)} B`);
  }
  if (budget === undefined) {
    console.log(`  ${`${name} (not budgeted)`.padEnd(50)} ${String(total).padStart(9)} B`);
    return;
  }
  const ok = total <= budget;
  over ||= !ok;
  console.log(
    `  ${name.padEnd(50)} ${String(total).padStart(9)} B of ${budget} B, ${ok ? "ok" : "OVER BUDGET"}`,
  );
}

console.log("web-budget: gzip sizes");
for (const [name, keys] of Object.entries(groups)) {
  report(
    name,
    keys.map((k) => manifest[k]?.file ?? k),
    budgets[name as keyof typeof groups],
  );
}
const css = [...new Set(Object.values(manifest).flatMap((c) => c.css ?? []))];
report("css", css, budgets.css);
console.log(`  shiki languages: ${languages.length} chunks, loaded one by one as code needs them`);
report(
  "languages",
  languages.map((k) => manifest[k]?.file ?? k),
);

if (over) {
  console.error("web-budget: the bundle is over budget");
  process.exit(1);
}

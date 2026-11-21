import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, test } from "vitest";
import { buildPages } from "./runner-pages.ts";

let dirs: string[] = [];
afterEach(() => {
  for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
  dirs = [];
});

/** A runner build as Vite makes it, with index.html's placeholder. */
function build(html = readFileSync("runner/index.html", "utf8")): { from: string; to: string } {
  const root = mkdtempSync(join(tmpdir(), "runner-pages-"));
  dirs.push(root);
  const from = join(root, "dist-runner");
  mkdirSync(join(from, "assets"), { recursive: true });
  mkdirSync(join(from, "pyodide", "314.0.7"), { recursive: true });
  writeFileSync(join(from, "index.html"), html);
  writeFileSync(join(from, "assets", "javascript.worker-Iaz17faw.js"), "");
  writeFileSync(join(from, "assets", "index-BI_kYpgH.js"), "");
  writeFileSync(join(from, "pyodide", "314.0.7", "pyodide.asm.wasm"), "");
  return { from, to: join(root, "dist-pages") };
}

test("the site names the app it was made for, with no placeholder left", () => {
  const { from, to } = build();
  buildPages(from, to, "https://ronote.example");
  const html = readFileSync(join(to, "index.html"), "utf8");
  expect(html).toContain(`<meta name="ronote-app-url" content='https://ronote.example' />`);
  expect(html).not.toContain("{{env");
  expect(readFileSync(join(to, "_headers"), "utf8")).toContain(
    "frame-ancestors https://ronote.example;",
  );
});

test("an app URL that isn't an https origin is refused", () => {
  const { from, to } = build();
  for (const url of ["http://ronote.example", "https://ronote.example/app", "ronote.example"]) {
    expect(() => buildPages(from, to, url), url).toThrow();
  }
});

// Only the app's URL is known at deploy; another name would ship empty.
test("a page naming another placeholder is refused", () => {
  const { from, to } = build(`<meta name="x" content='{{env "RONOTE_RUNNER_URL"}}' />`);
  expect(() => buildPages(from, to, "https://ronote.example")).toThrow(/RONOTE_RUNNER_URL/);
});

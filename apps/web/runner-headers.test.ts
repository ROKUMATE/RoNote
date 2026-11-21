import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { caddyRunnerSite, headersFor, pagesHeaders, pagesHeadersFor } from "./runner-headers.ts";

const app = "https://ronote.example";

// A built runner's kinds of files: the page, its chunks, both workers, and
// Pyodide's under its version.
const paths = [
  "/",
  "/index.html",
  "/assets/index-BI_kYpgH.js",
  "/assets/python.worker-CC90rYFV.js",
  "/assets/javascript.worker-Iaz17faw.js",
  "/assets/__vite-browser-external-3ZTq1Cy2.js",
  "/pyodide/314.0.7/pyodide.asm.wasm",
  "/pyodide/314.0.7/pyodide.asm.mjs",
  "/pyodide/314.0.7/python_stdlib.zip",
  "/pyodide/314.0.7/pyodide-lock.json",
];

// Pages joins a header two rules set with a comma, so the worker would get
// both policies, and the page's, which forbids evaluating, would win.
test("Pages sends every file exactly one CSP, which only the worker's lets evaluate", () => {
  const text = pagesHeaders(app);
  for (const path of paths) {
    const csp = pagesHeadersFor(text, path)["Content-Security-Policy"] ?? "";
    expect(csp.split("default-src").length, `${path}: ${csp}`).toBe(2);
    expect(csp.includes("'unsafe-eval'"), path).toBe(path.includes("javascript.worker"));
  }
});

test("Pages sends what the dev server and Caddy send, file by file", () => {
  const text = pagesHeaders(app);
  for (const path of paths)
    expect(pagesHeadersFor(text, path), path).toEqual(headersFor(path, app));
});

// make generate writes it; a hand edit would let Caddy drift from Pages.
test("deploy/caddy/runner.caddy is the one definition's", () => {
  expect(readFileSync("../../deploy/caddy/runner.caddy", "utf8")).toBe(caddyRunnerSite());
});

test("the rules fit Pages' limits", () => {
  const text = pagesHeaders(app);
  expect(text.split("\n").filter((line) => line.startsWith("/")).length).toBeLessThanOrEqual(100);
  for (const line of text.split("\n")) expect(line.length).toBeLessThanOrEqual(2000);
});

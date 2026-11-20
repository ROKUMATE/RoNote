import { join } from "node:path";
import { expect, type Page, test } from "@playwright/test";

/*
 * Through Caddy, as people will use RoNote (docs/adr/0014-production-topology.md):
 * the app at ronote.test, the runner at ronote-run.test, the fakes behind
 * them. The flow signs in, imports, runs Python, searches, chats and
 * uploads a PDF, with no CSP violation; the headers each domain sends are
 * checked file by file. The worker imports pages from Caddy's fixture site
 * (deploy/quadlet/ci/config/sites.caddy).
 */

const app = "https://ronote.test";
const runner = "https://ronote-run.test";
const fixtures = "http://ronote-caddy:8080";
const pdfs = join(import.meta.dirname, "..", "..", "..", "testdata", "pdf");

const appCSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: data:; font-src 'self'; connect-src 'self'; frame-src https://ronote-run.test; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'";
const runnerCSP = (evaluates: boolean) =>
  `default-src 'self'; script-src 'self' ${evaluates ? "'unsafe-eval'" : "'wasm-unsafe-eval'"}; connect-src 'self'; frame-ancestors ${app}; base-uri 'none'; form-action 'none'`;

/** Every response of either domain carries these. */
const everywhere = {
  "strict-transport-security": "max-age=63072000; includeSubDomains",
  "x-content-type-options": "nosniff",
  "permissions-policy":
    "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
};
const appHeaders = {
  ...everywhere,
  "referrer-policy": "strict-origin-when-cross-origin",
  "cross-origin-opener-policy": "same-origin",
  "cross-origin-embedder-policy": "credentialless",
};
const runnerHeaders = {
  ...everywhere,
  "referrer-policy": "no-referrer",
  "cross-origin-opener-policy": "same-origin",
  "cross-origin-embedder-policy": "require-corp",
  "cross-origin-resource-policy": "cross-origin",
};

type Headers = Record<string, string>;

/**
 * Fetches each path from page's own origin, past the browser's cache, which
 * keeps no Strict-Transport-Security, and returns its headers.
 */
function headersOf(page: Page, paths: string[]): Promise<Record<string, Headers>> {
  return page.evaluate(async (paths) => {
    const out: Record<string, Headers> = {};
    for (const path of paths) {
      const response = await fetch(path, { credentials: "include", cache: "no-store" });
      out[path] = { status: String(response.status), ...Object.fromEntries(response.headers) };
    }
    return out;
  }, paths);
}

function expectHeaders(got: Headers, want: Headers, where: string) {
  for (const [name, value] of Object.entries(want)) {
    expect(got[name], `${where}: ${name}`).toBe(value);
  }
  expect(got.server, `${where}: server`).toBeUndefined();
}

/** Text of path on page's origin. */
function textOf(page: Page, path: string): Promise<string> {
  return page.evaluate((path) => fetch(path).then((r) => r.text()), path);
}

test.describe.configure({ mode: "serial" });

let page: Page;
const violations: string[] = [];

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
  page.on("console", (message) => {
    if (/Content.Security.Policy|Refused to/i.test(message.text())) violations.push(message.text());
  });
});

test.afterAll(async () => {
  await page.close();
});

test("sign in through the fake GitHub", async () => {
  await page.goto("/");
  await expect(page).toHaveURL(new RegExp(`^${app}/login`));
  await page.getByRole("link", { name: "Continue with GitHub" }).click();
  await expect(page).toHaveURL(/^https:\/\/idp\.ronote\.test\//);
  await page.getByRole("button", { name: "Sign in as e2e-w0", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
});

test("import a page and run its Python on the runner's domain, isolated", async () => {
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByLabel("Page URL").fill(`${fixtures}/linked/python/`);
  await dialog.getByRole("button", { name: "Import", exact: true }).click();
  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}$/, { timeout: 30_000 });
  const block = page.locator(".code-block").first();
  await block.getByRole("button", { name: "Run" }).click();
  await expect(block.getByRole("status")).toHaveText(/^Ran in /, { timeout: 60_000 });

  const frame = page.frameLocator(`iframe[src^="${runner}/"]`);
  expect(await page.evaluate(() => crossOriginIsolated)).toBe(true);
  await expect(frame.locator("body")).toBeAttached();
  const runnerFrame = page.frames().find((f) => f.url().startsWith(`${runner}/`));
  expect(await runnerFrame?.evaluate(() => crossOriginIsolated)).toBe(true);
});

test("import a documentation page, search it and chat with a citation", async () => {
  await page.goto("/");
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByLabel("Page URL").fill(`${fixtures}/pages/real-python-controlflow/`);
  await dialog.getByRole("button", { name: "Import", exact: true }).click();
  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}$/, { timeout: 30_000 });
  await expect(page.getByRole("article")).toBeVisible();

  // Indexing waits for edits to settle, then the fake embedder: search
  // again until it finds the page.
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Search your library" });
  const query = palette.getByRole("combobox", { name: "Search your library" });
  const hit = palette.getByRole("option").filter({ hasText: "for Statements" }).first();
  await expect
    .poll(
      async () => {
        await query.fill("");
        await query.fill("for statements");
        return hit.waitFor({ timeout: 3_000 }).then(
          () => true,
          () => false,
        );
      },
      { timeout: 45_000 },
    )
    .toBe(true);
  // Choosing it lands on its block, and closes the palette.
  await hit.click();
  await expect(palette).toBeHidden();
  await expect(page).toHaveURL(/\?block=/);

  await page.keyboard.press("ControlOrMeta+j");
  const panel = page.getByRole("dialog", { name: "Chat" });
  await expect(panel.getByRole("radio", { name: "This document" })).toBeChecked();
  const box = panel.getByRole("textbox", { name: "Ask your library" });
  await box.fill("How do for statements iterate?");
  await box.press("Enter");
  await expect(panel.getByRole("button", { name: /^Source 1: / })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Ask" })).toBeVisible();
  await page.keyboard.press("Escape");
});

test("upload a PDF, whose original keeps the API's sandbox", async () => {
  await page.goto("/");
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByRole("tab", { name: "Upload PDF" }).click();
  await dialog.getByLabel("Choose a PDF").setInputFiles(join(pdfs, "think-python-iteration.pdf"));
  await dialog.getByRole("button", { name: "Upload" }).click();
  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}$/, { timeout: 60_000 });
  const id = new URL(page.url()).pathname.replace("/doc/", "");

  const original = (await headersOf(page, [`/api/v1/documents/${id}/original`]))[
    `/api/v1/documents/${id}/original`
  ];
  expect(original?.status).toBe("200");
  expect(original?.["content-type"]).toBe("application/pdf");
  expect(original?.["content-security-policy"]).toBe("sandbox; default-src 'none'");
  expectHeaders(original ?? {}, appHeaders, "/original");
});

test("the app's files and API answers carry the app's headers", async () => {
  await page.goto("/");
  const html = await textOf(page, "/");
  const asset = /src="(\/assets\/index-[^"]+\.js)"/.exec(html)?.[1] ?? "";
  expect(asset).not.toBe("");
  expect(html).toContain(`<meta name="ronote-runner-url" content='${runner}' />`);

  const got = await headersOf(page, ["/", "/doc/anything", asset, "/boot.js", "/api/v1/me"]);
  for (const path of ["/", "/doc/anything"]) {
    expectHeaders(got[path] ?? {}, { ...appHeaders, "content-security-policy": appCSP }, path);
    expect(got[path]?.["cache-control"], path).toBe("no-cache");
  }
  expectHeaders(got[asset] ?? {}, { ...appHeaders, "content-security-policy": appCSP }, asset);
  expect(got[asset]?.["cache-control"]).toBe("public, max-age=31536000, immutable");
  expect(got[asset]?.["content-encoding"]).toBe("br");
  // The API's answers are JSON, with no page policy of Caddy's.
  expectHeaders(got["/api/v1/me"] ?? {}, appHeaders, "/api/v1/me");
  expect(got["/api/v1/me"]?.["content-security-policy"]).toBeUndefined();
});

test("the runner's files carry its own policy, and only the JavaScript worker may evaluate", async () => {
  await page.goto(`${runner}/`);
  const html = await textOf(page, "/");
  expect(html).toContain(`<meta name="ronote-app-url" content='${app}' />`);
  const main = /src="(\/assets\/index-[^"]+\.js)"/.exec(html)?.[1] ?? "";
  const code = await textOf(page, main);
  const jsWorker = /\/assets\/javascript\.worker-[\w-]+\.js/.exec(code)?.[0] ?? "";
  const pyWorker = /\/assets\/python\.worker-[\w-]+\.js/.exec(code)?.[0] ?? "";
  // Ours carries the version; Pyodide's own code names its CDN's paths too.
  const pyodide = /["'`](\/pyodide\/\d[\d.]*\/)["'`]/.exec(await textOf(page, pyWorker))?.[1] ?? "";
  expect([main, jsWorker, pyWorker, pyodide]).not.toContain("");

  const paths = [
    "/",
    main,
    jsWorker,
    pyWorker,
    `${pyodide}pyodide.asm.wasm`,
    `${pyodide}pyodide.asm.mjs`,
  ];
  const got = await headersOf(page, paths);
  for (const path of paths) {
    const evaluates = path === jsWorker;
    expectHeaders(
      got[path] ?? {},
      { ...runnerHeaders, "content-security-policy": runnerCSP(evaluates) },
      path,
    );
    expect(got[path]?.["content-security-policy"]?.includes("'unsafe-eval'"), path).toBe(evaluates);
  }
  const wasm = got[`${pyodide}pyodide.asm.wasm`] ?? {};
  expect(wasm["content-type"]).toBe("application/wasm");
  expect(wasm["content-encoding"]).toBe("br");
  expect(wasm["cache-control"]).toBe("public, max-age=31536000, immutable");
});

test("a body over the API's limit stops at Caddy", async () => {
  await page.goto("/");
  const got = await page.evaluate(async () => {
    const response = await fetch("/api/v1/conversations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "x".repeat(9 << 20),
    });
    // The API names every answer it sends; Caddy's own answer has no id.
    return { status: response.status, requestID: response.headers.get("x-request-id") };
  });
  expect(got).toEqual({ status: 413, requestID: null });
});

test("no page broke its Content Security Policy", () => {
  expect(violations).toEqual([]);
});

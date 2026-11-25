import { readFileSync, statSync, writeFileSync } from "node:fs";
import { basename } from "node:path";
import { expect, type Page, test } from "@playwright/test";
import type { components } from "../src/api/schema";

/*
 * The worst PDF the units accept (scripts/worst-pdf.ts: as many pages of
 * code and tables as the cap, docling.MaxPages), imported through the real docling and Ollama
 * (.github/workflows/arm-real.yml). It times each step of the import against
 * docling's 540-second document timeout, and searches all the while, so
 * Ollama embeds queries while it embeds the import's batches. What it
 * measured goes to RONOTE_WORST_REPORT as Markdown for the job's summary,
 * whether the import succeeds or not. make prod-worst-pdf runs it.
 */

const pdf = process.env.RONOTE_WORST_PDF;
const report = process.env.RONOTE_WORST_REPORT;
const pages = process.env.RONOTE_WORST_PAGES;
const documentTimeout = 540;

// The import job's three attempts of 10 minutes each, and the indexing.
test.setTimeout(40 * 60_000);

type Import = components["schemas"]["Import"];
/** Where the import is: its status, or uploading before it has one. */
type Progress = Pick<Import, "error_code" | "document_id"> & {
  status: Import["status"] | "uploading";
};
type ImportResponse = components["schemas"]["ImportResponse"];
type Search = components["schemas"]["SearchResults"];
type Answer<T> = { status: number; body: T; ms: number };

/** A GET from the signed-in page, as the app makes it: Chromium resolves *.test and trusts Caddy's CA. */
function get<T>(page: Page, path: string): Promise<Answer<T>> {
  return page.evaluate(async (path) => {
    const t = performance.now();
    const res = await fetch(path, { cache: "no-store" });
    return { status: res.status, body: await res.json(), ms: performance.now() - t };
  }, path);
}

const queries = ["for statements", "slices and arrays", "SELECT with a WHERE clause", "tuples"];
const query = (i: number) => queries[i % queries.length] ?? "tuples";

test("the worst accepted PDF converts within docling's timeout and is found", async ({ page }) => {
  expect(pdf, "RONOTE_WORST_PDF names the PDF").toBeTruthy();
  expect(report, "RONOTE_WORST_REPORT names the report").toBeTruthy();
  await page.goto("/");
  await page.getByRole("link", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: "Sign in as e2e-w1", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();

  const started = Date.now();
  const seconds = (at: number) => Math.round((at - started) / 1000);
  const entered = new Map<string, number>();
  const searches = { count: 0, degraded: 0, slowest: 0 };
  let last: Progress = { status: "uploading" };
  let found: number | undefined;

  const search = async (q: string, documentID?: string) => {
    const scope = documentID ? `&document_id=${documentID}` : "";
    const { status, body, ms } = await get<Search>(
      page,
      `/api/v1/search?q=${encodeURIComponent(q)}${scope}`,
    );
    expect(status, `search for ${q}`).toBe(200);
    searches.count++;
    searches.slowest = Math.max(searches.slowest, ms);
    if (body.degraded) {
      searches.degraded++;
    }
    return body;
  };

  const write = () => {
    const span = (from: string, to: string) => {
      const a = entered.get(from);
      const b = entered.get(to);
      return a === undefined || b === undefined ? "-" : `${seconds(b) - seconds(a)} s`;
    };
    const converting = entered.get("converting");
    // Converting ends at extracting, or at the failure when that came first.
    const failedConverting = !entered.has("extracting") && entered.has("failed");
    const converted = entered.get("extracting") ?? entered.get("failed");
    const convertSeconds =
      converting !== undefined && converted !== undefined
        ? seconds(converted) - seconds(converting)
        : undefined;
    const lines = [
      "### The worst accepted PDF",
      "",
      `\`${basename(pdf ?? "")}\`: ${pages ?? "?"} pages, ${((statSync(pdf ?? "").size || 0) / 1048576).toFixed(1)} MiB; the import ended **${last.status}**${last.error_code ? ` (${last.error_code})` : ""}.`,
      "",
      "| step | time |",
      "|---|---|",
      `| queued, until converting | ${span("queued", "converting")} |`,
      `| converting: pdftext's inspection, docling, pdftext's lines | ${convertSeconds === undefined ? "-" : `${convertSeconds} s${failedConverting ? ", then failed" : ""}, ${Math.round((convertSeconds * 100) / documentTimeout)}% of docling's ${documentTimeout} s`} |`,
      `| extracting, until done | ${span("extracting", "done")} |`,
      `| done, until found by search (indexed) | ${found === undefined || entered.get("done") === undefined ? "-" : `${seconds(found) - seconds(entered.get("done") ?? 0)} s`} |`,
      `| upload, until found | ${found === undefined ? "-" : `${seconds(found)} s`} |`,
      "",
      `${searches.count} searches meanwhile, ${searches.degraded} degraded (Ollama not answering); the slowest took ${(searches.slowest / 1000).toFixed(1)} s.`,
      "",
    ];
    writeFileSync(report ?? "", lines.join("\n"));
  };

  try {
    const upload = await page.evaluate(
      async ({ data, name }) => {
        const bytes = Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
        const form = new FormData();
        form.append("file", new Blob([bytes], { type: "application/pdf" }), name);
        const res = await fetch("/api/v1/imports/pdf", { method: "POST", body: form });
        return { status: res.status, body: (await res.json()) as ImportResponse };
      },
      { data: readFileSync(pdf ?? "").toString("base64"), name: basename(pdf ?? "") },
    );
    expect(upload.status, "the upload is queued").toBe(202);
    const { id } = upload.body.import;
    entered.set("queued", Date.now());

    // Every 2 s: the import's status, and a search over the whole library.
    for (let i = 0; last.status !== "done" && last.status !== "failed"; i++) {
      const res = await get<ImportResponse>(page, `/api/v1/imports/${id}`);
      expect(res.status).toBe(200);
      last = res.body.import;
      if (!entered.has(last.status)) {
        entered.set(last.status, Date.now());
      }
      await search(query(i));
      await page.waitForTimeout(2000);
    }
    expect(last.status, `the import failed: ${last.error_code}`).toBe("done");

    // Indexed: found by its words and its meaning, while the searches go on.
    for (let i = 0; found === undefined; i++) {
      const body = await search("for statements", last.document_id);
      if (body.results.length > 0 && !body.degraded) {
        found = Date.now();
        break;
      }
      await search(query(i));
      expect(
        Date.now() - (entered.get("done") ?? started),
        "indexed within 15 minutes",
      ).toBeLessThan(15 * 60_000);
      await page.waitForTimeout(2000);
    }
  } finally {
    write();
  }
  expect(searches.degraded, "every search had Ollama's answer").toBe(0);
});

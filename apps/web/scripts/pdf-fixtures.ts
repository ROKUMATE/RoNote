import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium, type Page } from "@playwright/test";

/*
 * Prints the PDF import's fixtures (docs/adr/0013-pdf-import.md) into
 * testdata/pdf: extractor golden pages printed to PDF by Chromium, so the
 * verbatim report can compare each PDF's code with the page's, and the
 * hostile inputs made the same way. make pdf-fixtures runs it, then makes
 * encrypted.pdf with qpdf. Chromium stamps each PDF with its time, so a new
 * print is a new file: run make pdf-record after it.
 *
 *   node scripts/pdf-fixtures.ts [REPO]     REPO defaults to ../..
 */

const repo = process.argv[2] ?? "../..";
const out = join(repo, "testdata", "pdf");

/**
 * Golden pages whose code blocks all show in print, as the extractor reads
 * them: no tabs hiding blocks, no line numbers it leaves out, and no block
 * the extractor rewrites (FastAPI's terminal blocks, ADR 0004). The
 * verbatim report reads the same list.
 */
const printed: string[] = JSON.parse(readFileSync(join(out, "printed.json"), "utf8"));

/**
 * The page as its site shows it, without the site's stylesheet: what it
 * hides stays hidden (mdbook's boring lines). Code keeps its lines, in a
 * font small enough that they fit the page, and a block stays on one page
 * when it fits.
 */
const printCSS = [
  ".boring { display: none; }",
  "pre { white-space: pre; overflow: visible; break-inside: avoid; font-size: 7pt; }",
].join("\n");

const margin = { top: "15mm", bottom: "15mm", left: "15mm", right: "15mm" };

async function print(page: Page, html: string, file: string) {
  await page.setContent(html, { waitUntil: "load" });
  await page.addStyleTag({ content: printCSS });
  await page.pdf({ path: join(out, file), format: "A4", margin });
}

const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  // A golden page prints as saved: nothing it links to is fetched.
  await page.route("**/*", (route) =>
    route.request().url().startsWith("data:") ? route.continue() : route.abort(),
  );
  for (const name of printed) {
    const html = readFileSync(join(repo, "testdata", "golden", name, "input.html"), "utf8");
    await print(page, html, `${name}.pdf`);
  }

  // Over the page cap: refused at inspection, before docling sees it.
  const many = Array.from(
    { length: 301 },
    (_, i) => `<p style="break-after: page">Page ${i + 1}</p>`,
  );
  await print(page, many.join(""), "pages-301.pdf");

  // A scan: the page as a picture, with no text layer.
  await page.setContent(
    "<h1>Chapter 1</h1><pre>for i in range(3):\n    print(i)</pre><p>A page that was scanned.</p>",
  );
  const picture = (await page.screenshot({ type: "png" })).toString("base64");
  await print(
    page,
    `<img src="data:image/png;base64,${picture}" style="width: 100%">`,
    "image-only.pdf",
  );

  // Recorded with a tiny docling timeout, so its answer is docling's own.
  const slow = readFileSync(
    join(repo, "testdata", "golden", "mdbook-v04-reading-files", "input.html"),
    "utf8",
  );
  await print(page, slow, "timeout.pdf");
} finally {
  await browser.close();
}

// A web page saved with a .pdf name: refused before docling sees it.
writeFileSync(
  join(out, "not-a-pdf.pdf"),
  "<!doctype html><title>Not a PDF</title><p>This is HTML.</p>\n",
);

import { readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium, type Page } from "@playwright/test";

/*
 * Prints the worst PDF the units accept (.github/workflows/arm-real.yml): the
 * PDF fixtures' golden pages, with their code blocks and tables, and the
 * tables' golden, one after another, each from a new page, until the next
 * would pass PAGES, printed by Chromium as make pdf-fixtures prints them. It
 * stays under docling's 300 pages (DOCLING_SERVE_MAX_NUM_PAGES) and RoNote's
 * 25 MB, and fails if it doesn't. It is never committed: CI prints it.
 *
 *   node scripts/worst-pdf.ts OUT [PAGES] [REPO]    PAGES 298, REPO ../..
 */

const [out, pagesArg = "298", repo = "../.."] = process.argv.slice(2);
if (!out) {
  throw new Error("usage: node scripts/worst-pdf.ts OUT [PAGES] [REPO]");
}
const target = Number(pagesArg);
const maxPages = 300;
const maxBytes = 25 << 20;

const golden = join(repo, "testdata", "golden");
const names: string[] = [
  ...JSON.parse(readFileSync(join(repo, "testdata", "pdf", "printed.json"), "utf8")),
  "table",
];

// The fixtures' print settings (scripts/pdf-fixtures.ts).
const printCSS = [
  ".boring { display: none; }",
  "pre { white-space: pre; overflow: visible; break-inside: avoid; font-size: 7pt; }",
  "section + section { break-before: page; }",
].join("\n");
const margin = { top: "15mm", bottom: "15mm", left: "15mm", right: "15mm" };

/** A golden page's body, without its scripts. */
function body(name: string): string {
  const html = readFileSync(join(golden, name, "input.html"), "utf8");
  const inner = /<body[^>]*>([\s\S]*)<\/body>/i.exec(html)?.[1] ?? html;
  return inner.replace(/<script[\s\S]*?<\/script>/gi, "");
}

/** Chromium writes each page as an uncompressed /Type /Page object. */
function pageCount(pdf: Buffer): number {
  return pdf.toString("latin1").match(/\/Type \/Page(?![s\w])/g)?.length ?? 0;
}

async function print(page: Page, sections: string[]): Promise<Buffer> {
  await page.setContent(sections.map((s) => `<section>${s}</section>`).join(""), {
    waitUntil: "load",
  });
  await page.addStyleTag({ content: printCSS });
  return page.pdf({ format: "A4", margin });
}

const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.route("**/*", (route) =>
    route.request().url().startsWith("data:") ? route.continue() : route.abort(),
  );
  const sections = names.map((name) => ({ name, html: body(name), pages: 0 }));
  for (const s of sections) {
    s.pages = pageCount(await print(page, [s.html]));
  }

  // Round after round, each section that still fits; done when none does.
  const chosen: string[] = [];
  let total = 0;
  for (let added = true; added; ) {
    added = false;
    for (const s of sections) {
      if (total + s.pages <= target) {
        chosen.push(s.html);
        total += s.pages;
        added = true;
      }
    }
  }
  const pdf = await print(page, chosen);
  writeFileSync(out, pdf);
  const pages = pageCount(pdf);
  const bytes = statSync(out).size;
  console.log(
    `worst-pdf: ${out}: ${pages} pages, ${(bytes / 1048576).toFixed(1)} MiB, from ${chosen.length} golden pages (${sections.map((s) => `${s.name} ${s.pages}`).join(", ")})`,
  );
  if (pages > maxPages || bytes > maxBytes || pages < target - 10) {
    throw new Error(
      `worst-pdf: ${pages} pages and ${bytes} bytes; want ${target - 10} to ${maxPages} pages and at most ${maxBytes} bytes`,
    );
  }
} finally {
  await browser.close();
}

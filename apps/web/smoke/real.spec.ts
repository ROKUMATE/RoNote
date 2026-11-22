import { join } from "node:path";
import { expect, test } from "@playwright/test";

/*
 * The real docling and Ollama (docs/adr/0015-operations.md,
 * .github/workflows/arm-real.yml), through Caddy as the smoke suite goes: a
 * PDF converted by docling's real models, and found by a search whose meaning
 * half Ollama's embeddings answered. Only GitHub's sign-in and the chat model
 * are the fakes. make prod-real-smoke runs it.
 */

const pdfs = join(import.meta.dirname, "..", "..", "..", "testdata", "pdf");

// docling's own limit on one document is 9 minutes (its unit); the
// conversion, then the indexing, which waits 5 s for edits to settle and
// embeds on the CPU, are given 15 together.
test.setTimeout(15 * 60_000);

test("a PDF is converted by the real docling and found by search over the real embedder", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: "Sign in as e2e-w0", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();

  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByRole("tab", { name: "Upload PDF" }).click();
  await dialog.getByLabel("Choose a PDF").setInputFiles(join(pdfs, "real-python-controlflow.pdf"));
  await dialog.getByRole("button", { name: "Upload" }).click();
  await expect(page, "docling converts the PDF within its 9 minutes").toHaveURL(
    /\/doc\/[0-9a-f-]{36}$/,
    { timeout: 10 * 60_000 },
  );
  const id = new URL(page.url()).pathname.replace("/doc/", "");

  // The page's words and its meaning both searched: degraded would mean
  // Ollama didn't answer, and the hits were word matches alone.
  await expect
    .poll(
      () =>
        page.evaluate(
          (path) =>
            fetch(path, { cache: "no-store" })
              .then((r) => r.json())
              .then((body: { results: unknown[]; degraded: boolean }) =>
                body.degraded ? -1 : body.results.length,
              ),
          `/api/v1/search?q=${encodeURIComponent("for statements")}&document_id=${id}`,
        ),
      {
        timeout: 5 * 60_000,
        message:
          "the PDF is indexed and found by words and by meaning (-1 is Ollama not answering)",
      },
    )
    .toBeGreaterThan(0);
});

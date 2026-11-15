import { join } from "node:path";
import type { Page } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  importTimeout,
} from "./helpers";
import { expect, signIn, ownUserTest as test, workerLogin } from "./test";

/*
 * PDF import (docs/adr/0013-pdf-import.md), converted by cmd/fakedocling
 * from the fixtures' recordings. Each test has a user of its own: the same
 * file uploaded twice is the same document.
 */

const fixtures = join(import.meta.dirname, "..", "..", "..", "testdata", "pdf");

declare global {
  interface Window {
    /** Each step the import dialog marked current, in the order it did. */
    importSteps: string[];
  }
}

/** Uploads a PDF fixture from the import dialog; returns the dialog. */
async function upload(page: Page, file: string) {
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByRole("tab", { name: "Upload PDF" }).click();
  await dialog.getByLabel("Choose a PDF").setInputFiles(join(fixtures, file));
  await dialog.getByRole("button", { name: "Upload" }).click();
  return dialog;
}

/** Uploads the Think Python chapter and waits for its document. */
async function openChapter(page: Page) {
  await page.goto("/");
  await upload(page, "think-python-iteration.pdf");
  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}$/, { timeout: importTimeout });
  await expect(page.getByRole("link", { name: /^think-python-iteration\.pdf/ })).toBeVisible();
  return new URL(page.url()).pathname.split("/").at(-1) ?? "";
}

test("a PDF converts with its steps, and its code runs as printed", async ({ page }) => {
  await page.addInitScript(() => {
    window.importSteps = [];
    new MutationObserver(() => {
      const current = document.querySelector('[data-step][data-state="current"]');
      const step = current?.getAttribute("data-step");
      if (step && window.importSteps.at(-1) !== step) window.importSteps.push(step);
    }).observe(document, { subtree: true, childList: true, attributeFilter: ["data-state"] });
  });

  await openChapter(page);

  const steps = await page.evaluate(() => window.importSteps);
  const order = ["queued", "converting", "extracting", "done"];
  expect(steps[0], "the first step").toBe("queued");
  expect(
    steps.every((s) => order.includes(s)),
    "a PDF's steps",
  ).toBe(true);
  await expect(page.getByText("8 pages")).toBeVisible();
  await expect(page.getByRole("list", { name: "Blocks" })).toContainText("code blocks");

  // PDF code has no language: the reader picks Python, then runs it.
  const countdown = page.locator(".code-block").filter({ hasText: "def countdown(n):" });
  await countdown.getByRole("button", { name: /^Language:/ }).click();
  await page
    .getByRole("listbox", { name: "Code language" })
    .getByRole("option", { name: "Python", exact: true })
    .click();
  await countdown.getByRole("button", { name: "Run" }).click();
  await expect(countdown.getByRole("status")).toHaveText(/^Ran in /, { timeout: 20_000 });
});

test("View page N opens the original at the block's page, sandboxed", async ({ page }) => {
  const id = await openChapter(page);
  const countdown = page.locator(".code-block").filter({ hasText: "def countdown(n):" });

  await countdown.hover();

  const link = page.getByRole("link", { name: /^View page 3/ });
  await expect(link).toHaveAttribute("href", `/api/v1/documents/${id}/original#page=3`);
  await expect(link).toHaveAttribute("target", "_blank");
  // Headless Chromium shows no PDF: what the link opens is checked as a
  // response, and landing on the page in a real browser.
  const original = await page.request.get(`/api/v1/documents/${id}/original`);
  expect(original.status()).toBe(200);
  expect(original.headers()).toMatchObject({
    "content-type": "application/pdf",
    "x-content-type-options": "nosniff",
    "content-security-policy": "sandbox; default-src 'none'",
    "content-disposition": `inline; filename="think-python-iteration.pdf"; filename*=UTF-8''think-python-iteration.pdf`,
  });
  expect((await original.body()).subarray(0, 5).toString()).toBe("%PDF-");
});

test("another user gets 404 for the original", async ({ page, browser }) => {
  const id = await openChapter(page);
  const other = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  try {
    const theirs = await other.newPage();
    await signIn(theirs, workerLogin());

    const res = await theirs.request.get(`/api/v1/documents/${id}/original`);

    expect(res.status()).toBe(404);
    expect(res.headers()["content-type"]).not.toBe("application/pdf");
  } finally {
    await other.close();
  }
});

test("a scanned PDF says it can't be read yet", async ({ page }) => {
  await page.goto("/");
  const dialog = await upload(page, "image-only.pdf");

  await expect(dialog.getByRole("alert")).toHaveText(
    "That PDF is scanned pages without text, which RoNote can't read yet.",
    { timeout: importTimeout },
  );
});

test("a file that isn't a PDF is refused before it is sent", async ({ page }) => {
  await page.goto("/");
  let sent = false;
  page.on("request", (r) => {
    if (r.url().endsWith("/api/v1/imports/pdf")) sent = true;
  });

  const dialog = await upload(page, "SOURCES.md");

  await expect(dialog.getByText("That file isn't a PDF.")).toBeVisible();
  expect(sent).toBe(false);
});

for (const theme of ["light", "dark"] as const) {
  test(`the upload and a PDF document pass axe in ${theme}, at 375px`, async ({ page }) => {
    await chooseTheme(page, theme);
    await page.setViewportSize({ width: 375, height: 800 });
    await page.goto("/");
    await page.getByRole("button", { name: "Import page" }).click();
    const dialog = page.getByRole("dialog", { name: "Import page" });
    await dialog.getByRole("tab", { name: "Upload PDF" }).click();
    // Checked once the dialog has faded in, its muted text at full contrast.
    await expect(dialog).toHaveCSS("opacity", "1");
    await expectNoAxeViolations(page);
    await page.keyboard.press("Escape");

    await openChapter(page);
    await page.locator(".code-block").filter({ hasText: "def countdown(n):" }).hover();
    await expect(page.getByRole("link", { name: /^View page 3/ })).toBeVisible();
    await expectNoAxeViolations(page);
    await expectNoHorizontalScroll(page);
  });
}

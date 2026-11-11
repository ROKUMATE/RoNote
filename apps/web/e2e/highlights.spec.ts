import type { Page } from "@playwright/test";
import type { Document } from "../src/api/schema";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  openLinkedPage,
} from "./helpers";
import { expect, test } from "./test";

// Paragraphs of e2e/fixtures/linked-a.html.
const intro = "A synthetic page for RoNote's end-to-end tests.";
const setup1 = "Setup paragraph 1: invented text";
const setup2 = "Setup paragraph 2: invented text";

type View = { posAtDOM(node: Node, offset: number): number };
type Editor = {
  view: View;
  chain(): {
    focus(): { setTextSelection(range: { from: number; to: number }): { run(): boolean } };
  };
};

/**
 * Selects, through the editor, from the start of quote in the paragraph
 * starting with para to the end of endQuote in the one starting with
 * endPara (the same paragraph by default).
 */
async function select(page: Page, para: string, quote: string, endPara = para, endQuote = quote) {
  await page.evaluate(
    ({ para, quote, endPara, endQuote }) => {
      const paragraphs = [...document.querySelectorAll<HTMLElement>(".ProseMirror p")];
      const find = (start: string) => {
        const p = paragraphs.find((el) => el.textContent?.startsWith(start));
        if (!p) throw new Error(`no paragraph starting ${start}`);
        return p;
      };
      const first = find(para);
      const last = find(endPara);
      const { editor } = first.closest(".ProseMirror") as unknown as { editor: Editor };
      const from = editor.view.posAtDOM(first, 0) + (first.textContent ?? "").indexOf(quote);
      const to =
        editor.view.posAtDOM(last, 0) +
        (last.textContent ?? "").indexOf(endQuote) +
        endQuote.length;
      editor.chain().focus().setTextSelection({ from, to }).run();
    },
    { para, quote, endPara, endQuote },
  );
  // The editor takes focus on the next frame; keys before it go nowhere.
  await expect(page.getByLabel("Document text")).toBeFocused();
}

const marks = (page: Page) => page.locator(".reader mark.hl");

/**
 * The text the editor has selected. The editor hears of a selection made
 * with the keyboard a moment after the browser makes it, from
 * selectionchange; keys pressed faster than a person can would act on the
 * selection before.
 */
function selected(page: Page) {
  return page.evaluate(() => {
    type State = {
      selection: { from: number; to: number };
      doc: { textBetween(from: number, to: number): string };
    };
    const { editor } = document.querySelector(".ProseMirror") as unknown as {
      editor: { state: State };
    };
    const { from, to } = editor.state.selection;
    return editor.state.doc.textBetween(from, to);
  });
}

/** Highlights the selection from the toolbar and waits for it to be saved. */
async function highlightSelection(page: Page, button = "Highlight") {
  const saved = page.waitForResponse(
    (r) => r.request().method() === "POST" && r.url().endsWith("/annotations") && r.ok(),
  );
  await page
    .getByRole("toolbar", { name: "Highlight" })
    .getByRole("button", { name: button, exact: true })
    .click();
  await saved;
}

/** Highlights the selection with a note, and waits for the note to be saved. */
async function highlightWithNote(page: Page, note: string) {
  await highlightSelection(page, "Highlight + note");
  const field = page
    .getByRole("dialog", { name: "Highlight" })
    .getByRole("textbox", { name: "Note" });
  await expect(field).toBeFocused();
  await field.fill(note);
  const saved = page.waitForResponse(
    (r) => r.request().method() === "PATCH" && r.url().includes("/annotations/") && r.ok(),
  );
  await field.press("Enter");
  await saved;
}

const notesMargin = (page: Page) => page.getByRole("complementary", { name: "Notes" });

test("a highlight is saved, and is still there after a reload", async ({ page }) => {
  await openLinkedPage(page);
  await select(page, intro, "synthetic page");

  await highlightSelection(page);

  await expect(marks(page)).toHaveText(["synthetic page"]);
  await page.reload();
  await expect(marks(page)).toHaveText(["synthetic page"]);
});

test("text typed before a highlight leaves it on the same words, after a reload too", async ({
  page,
}) => {
  await openLinkedPage(page);
  await select(page, setup1, "invented text");
  await highlightSelection(page);
  const moved = page.waitForResponse((r) => r.url().endsWith("/annotations/reanchor") && r.ok(), {
    timeout: 10_000,
  });

  await select(page, setup1, "Setup", setup1, "");
  await page.keyboard.type("Early ");

  await moved;
  await expect(marks(page)).toHaveText(["invented text"]);
  await page.reload();
  await expect(page.getByText(/^Early Setup paragraph 1/)).toBeVisible();
  await expect(marks(page)).toHaveText(["invented text"]);
});

test("Remove can be undone", async ({ page }) => {
  await openLinkedPage(page);
  await select(page, intro, "end-to-end tests");
  await highlightSelection(page);

  await marks(page).click();
  await page
    .getByRole("dialog", { name: "Highlight" })
    .getByRole("button", { name: "Remove highlight" })
    .click();

  await expect(marks(page)).toHaveCount(0);
  await page
    .getByRole("status")
    .filter({ hasText: "Highlight removed" })
    .getByRole("button", { name: "Undo" })
    .click();
  await expect(marks(page)).toHaveText(["end-to-end tests"]);
  await page.reload();
  await expect(marks(page)).toHaveText(["end-to-end tests"]);
});

test("a selection across two paragraphs makes one highlight in each", async ({ page }) => {
  await openLinkedPage(page);
  await select(page, setup1, "only makes", setup2, "invented text");
  const toolbar = page.getByRole("toolbar", { name: "Highlight" });
  await expect(toolbar.getByRole("button", { name: "Highlight + note" })).toHaveCount(0);

  await highlightSelection(page);

  await expect(marks(page)).toHaveCount(2);
  const ids = await marks(page).evaluateAll((els) =>
    els.map((e) => e.getAttribute("data-annotation")),
  );
  expect(new Set(ids).size).toBe(2);
  await expect(marks(page).first()).toHaveText(/^only makes the page long enough/);
  await expect(marks(page).last()).toHaveText("Setup paragraph 2: invented text");
});

test("deleting the highlighted words leaves the highlight unplaced, never lost; Re-apply puts it back", async ({
  page,
}) => {
  await openLinkedPage(page);
  await select(page, setup1, "invented text");
  await highlightWithNote(page, "Keep me");
  const saved = page.waitForResponse(
    (r) => r.request().method() === "PATCH" && r.url().includes("/documents/") && r.ok(),
    { timeout: 10_000 },
  );

  await select(page, setup1, "invented text");
  await page.keyboard.press("Backspace");
  await saved;

  await expect(marks(page)).toHaveCount(0);
  const list = page.locator("details").filter({ hasText: /^Highlights \(1\)/ });
  await list.getByText("Highlights (1)").click();
  const unplaced = list.getByRole("region", { name: "Unplaced highlights" });
  await expect(unplaced.getByText("invented text")).toBeVisible();
  await expect(unplaced.getByText("Keep me")).toBeVisible();
  await page.reload();
  await page.getByText("Highlights (1)").click();
  await expect(unplaced.getByText("invented text")).toBeVisible();

  await unplaced.getByRole("button", { name: "Re-apply" }).click();
  // Its words are gone from the paragraph, so it no longer starts with them.
  await select(page, "Setup paragraph 1:", "only makes the page");
  const placed = page.waitForResponse(
    (r) => r.request().method() === "PATCH" && r.url().includes("/annotations/") && r.ok(),
  );
  await page
    .getByRole("toolbar", { name: "Highlight" })
    .getByRole("button", { name: "Place here" })
    .click();
  await placed;

  await expect(marks(page)).toHaveText(["only makes the page"]);
  await expect(unplaced).toHaveCount(0);
  await page.reload();
  await expect(marks(page)).toHaveText(["only makes the page"]);
});

test("a highlight in code persists, and Copy still copies the exact code", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const path = await openLinkedPage(page);
  const block = page.locator(".code-block").filter({ hasText: "func main()" });
  await block.locator("pre code").evaluate((code) => {
    // The browser's own selection, of "fmt.Println", inside the code.
    const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
    let at = 0;
    const start = (code.textContent ?? "").indexOf("fmt.Println");
    const range = document.createRange();
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const length = node.textContent?.length ?? 0;
      if (start >= at && start < at + length) range.setStart(node, start - at);
      if (start + 11 > at && start + 11 <= at + length) range.setEnd(node, start + 11 - at);
      at += length;
    }
    getSelection()?.removeAllRanges();
    getSelection()?.addRange(range);
  });

  await highlightSelection(page);

  await expect(block.locator("mark.hl")).toHaveText(["fmt.Println"]);
  await page.reload();
  await expect(block.locator("mark.hl")).toHaveText(["fmt.Println"]);
  const stored: Document = await (
    await page.request.get(`/api/v1${path.replace("/doc/", "/documents/")}`)
  ).json();
  const text = stored.content.blocks.find((b) => b.text?.includes("func main()"))?.text;
  await block.getByRole("button", { name: "Copy code" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(text);
  expect(await block.locator("pre").textContent()).toBe(text);
});

test("keyboard only: select, highlight, and add a note", async ({ page }) => {
  await openLinkedPage(page);
  await select(page, setup1, "Setup", setup1, "");
  for (const _ of "Setup") await page.keyboard.press("Shift+ArrowRight");
  await expect.poll(() => selected(page)).toBe("Setup");
  // The highlight controls load just after the page; they show over the
  // selection once they have.
  await expect(page.getByRole("toolbar", { name: "Highlight" })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+Shift+H");
  await expect(marks(page)).toHaveText(["Setup"]);

  // The caret is after "Setup" now: take the next word with its space.
  for (const _ of " paragraph") await page.keyboard.press("Shift+ArrowRight");
  await expect.poll(() => selected(page)).toBe(" paragraph");
  await page.keyboard.press("Alt+F10");
  const toolbar = page.getByRole("toolbar", { name: "Highlight" });
  await expect(toolbar.getByRole("button", { name: "Highlight", exact: true })).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(toolbar.getByRole("button", { name: "Highlight + note" })).toBeFocused();
  await page.keyboard.press("Enter");
  const note = page
    .getByRole("dialog", { name: "Highlight" })
    .getByRole("textbox", { name: "Note" });
  await expect(note).toBeFocused();
  await page.keyboard.type("Read this twice");
  const saved = page.waitForResponse(
    (r) => r.request().method() === "PATCH" && r.url().includes("/annotations/") && r.ok(),
  );
  await page.keyboard.press("Enter");

  await saved;
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(marks(page)).toHaveText(["Setup", " paragraph"]);

  // Closing the note gives the text its focus back; then to the list, open
  // it, and jump to the first highlight, by keyboard.
  await expect(page.getByLabel("Document text")).toBeFocused();
  await page.getByText("Highlights (2)").focus();
  await page.keyboard.press("Enter");
  const jump = page.locator("details").getByRole("button", { name: "Jump to" }).first();
  await expect(jump).toBeVisible();
  while (!(await jump.evaluate((el) => el === document.activeElement))) {
    await page.keyboard.press("Tab");
  }
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Document text")).toBeFocused();
  await expect.poll(() => selected(page)).toBe("");
  await expect(marks(page).first()).toBeInViewport();
});

test.describe("notes", () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test("a note shows in the margin, level with its highlight, after a reload too", async ({
    page,
  }) => {
    await openLinkedPage(page);
    await select(page, intro, "synthetic page");

    await highlightWithNote(page, "Ask about the fixtures");

    const card = notesMargin(page).getByRole("button", { name: /Ask about the fixtures/ });
    await expect(card).toBeVisible();
    const level = async () =>
      Math.abs(
        ((await card.boundingBox())?.y ?? 0) - ((await marks(page).first().boundingBox())?.y ?? 0),
      );
    await expect.poll(level).toBeLessThan(12);
    await page.reload();
    await expect(card).toBeVisible();
    await expect.poll(level).toBeLessThan(12);
  });

  test("notes that would overlap stack, one below the other", async ({ page }) => {
    await openLinkedPage(page);
    await select(page, setup1, "Setup");
    await highlightWithNote(
      page,
      "First note, long enough to take more than one line in the margin",
    );
    await select(page, setup1, "invented");
    await highlightWithNote(page, "Second note");

    const first = notesMargin(page).getByRole("button", { name: /First note/ });
    const second = notesMargin(page).getByRole("button", { name: /Second note/ });
    await expect(second).toBeVisible();
    const a = await first.boundingBox();
    const b = await second.boundingBox();
    expect((b?.y ?? 0) >= (a?.y ?? 0) + (a?.height ?? 0)).toBe(true);
  });

  test("below 1100px the margin folds away, and the note opens under its highlight", async ({
    page,
  }) => {
    await openLinkedPage(page);
    await select(page, intro, "synthetic page");
    await highlightWithNote(page, "Folded note");
    await expect(notesMargin(page).getByRole("button", { name: /Folded note/ })).toBeVisible();

    await page.setViewportSize({ width: 1099, height: 900 });

    await expect(notesMargin(page)).toBeHidden();
    const icon = page.getByRole("button", { name: "Note on “synthetic page”" });
    await expect(icon).toBeVisible();
    await icon.click();
    const field = page
      .getByRole("dialog", { name: "Highlight" })
      .getByRole("textbox", { name: "Note" });
    await expect(field).toHaveValue("Folded note");
    const below =
      ((await field.boundingBox())?.y ?? 0) - ((await marks(page).first().boundingBox())?.y ?? 0);
    expect(below).toBeGreaterThan(0);

    await page.setViewportSize({ width: 1100, height: 900 });
    await expect(notesMargin(page).getByRole("button", { name: /Folded note/ })).toBeVisible();
  });
});

test.describe("accessibility", () => {
  test.use({ reducedMotion: "reduce" });

  for (const theme of ["light", "dark"] as const) {
    test(`highlights, the toolbar and the popover have no axe violations in ${theme}`, async ({
      page,
    }) => {
      await chooseTheme(page, theme);
      await openLinkedPage(page);
      await select(page, intro, "synthetic page");
      await expect(page.getByRole("toolbar", { name: "Highlight" })).toBeVisible();
      await expectNoAxeViolations(page);

      await highlightWithNote(page, "A note for axe");
      await expect(notesMargin(page).getByRole("button", { name: /A note for axe/ })).toBeVisible();
      await marks(page).click();
      await expect(page.getByRole("dialog", { name: "Highlight" })).toBeVisible();
      await expectNoAxeViolations(page);

      await page.keyboard.press("Escape");
      await page.getByText("Highlights (1)").click();
      await expect(page.getByRole("button", { name: "Jump to" })).toBeVisible();
      await expectNoAxeViolations(page);
    });
  }

  test.describe("at 375px", () => {
    test.use({ viewport: { width: 375, height: 800 } });

    test("the toolbar and the popover fit the screen", async ({ page }) => {
      await openLinkedPage(page);
      await select(page, setup1, "only makes the page long enough to scroll");
      await expect(page.getByRole("toolbar", { name: "Highlight" })).toBeVisible();
      await expectNoHorizontalScroll(page);

      await highlightSelection(page);
      await marks(page).click();
      await expect(page.getByRole("dialog", { name: "Highlight" })).toBeVisible();
      await expectNoHorizontalScroll(page);
    });
  });
});

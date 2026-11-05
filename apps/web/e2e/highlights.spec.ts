import { expect, type Page, test } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  openLinkedPage,
} from "./helpers";

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

test("keyboard only: select, highlight, and add a note", async ({ page }) => {
  await openLinkedPage(page);
  await select(page, setup1, "Setup", setup1, "");
  // The editor takes focus on the next frame; keys before it go nowhere.
  await expect(page.getByLabel("Document text")).toBeFocused();
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

      await highlightSelection(page);
      await marks(page).click();
      await expect(page.getByRole("dialog", { name: "Highlight" })).toBeVisible();
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

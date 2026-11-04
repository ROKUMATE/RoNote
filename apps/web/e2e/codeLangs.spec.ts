import { expect, type Locator, type Page, test } from "@playwright/test";
import type { Document } from "../src/api/schema";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  openLinkedPage,
} from "./helpers";

/** Linked page A's code blocks, by a line each holds. */
function codeBlock(page: Page, text: string): Locator {
  return page.locator(".code-block").filter({ hasText: text });
}

function picker(block: Locator): Locator {
  return block.getByRole("button", { name: /^Language:/ });
}

/** Chooses lang for block, from its language listbox. */
async function choose(page: Page, block: Locator, lang: string, { forAll = false } = {}) {
  await picker(block).click();
  const listbox = page.getByRole("listbox", { name: "Code language" });
  await expect(listbox).toBeFocused();
  if (forAll) await page.getByRole("checkbox", { name: /^Use for all unlabelled code/ }).check();
  await listbox.getByRole("option", { name: lang, exact: true }).click();
  await expect(listbox).toBeHidden();
}

function highlighted(block: Locator): Locator {
  return block.locator("pre span[style*='color']").first();
}

async function saved(page: Page) {
  await expect(page.getByRole("status").filter({ hasText: /^Saved$/ })).toBeVisible();
}

test("a chosen language highlights the block, and is still there after a reload", async ({
  page,
}) => {
  await openLinkedPage(page);
  const goBlock = codeBlock(page, "func main()");
  await expect(picker(goBlock)).toHaveText("Language: plain text");
  await expect(highlighted(goBlock)).toHaveCount(0);

  await choose(page, goBlock, "Go");

  await expect(picker(goBlock)).toHaveText("Language: Go · yours");
  await expect(picker(goBlock)).toBeFocused();
  await expect(highlighted(goBlock)).toBeVisible();
  await saved(page);
  await page.reload();
  await expect(picker(goBlock)).toHaveText("Language: Go · yours");
  await expect(highlighted(goBlock)).toBeVisible();
});

test("the keyboard opens the list, moves through it and chooses", async ({ page }) => {
  await openLinkedPage(page);
  const sqlBlock = codeBlock(page, "SELECT id");

  await picker(sqlBlock).focus();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByRole("listbox", { name: "Code language" })).toBeFocused();
  await page.keyboard.press("s");
  await page.keyboard.press("s");
  await page.keyboard.press("s");
  await page.keyboard.press("Enter");

  await expect(picker(sqlBlock)).toHaveText("Language: SQL · yours");
  await expect(picker(sqlBlock)).toBeFocused();
  await expect(highlighted(sqlBlock)).toBeVisible();
});

test("Use for all highlights every unlabelled block, and Copy still copies the exact code", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const path = await openLinkedPage(page);
  const blocks = [
    codeBlock(page, "make build"),
    codeBlock(page, "func main()"),
    codeBlock(page, "SELECT id"),
  ];
  const [makeBlock, goBlock, sqlBlock] = blocks as [Locator, Locator, Locator];
  await choose(page, sqlBlock, "SQL");

  await choose(page, makeBlock, "Bash", { forAll: true });

  await expect(picker(makeBlock)).toHaveText("Language: Bash · yours");
  await expect(picker(goBlock)).toHaveText("Language: Bash · yours");
  await expect(picker(sqlBlock)).toHaveText("Language: SQL · yours");
  const stored = async (): Promise<Document> =>
    (await page.request.get(`/api/v1${path.replace("/doc/", "/documents/")}`)).json();
  const code = (doc: Document) => doc.content.blocks.filter((b) => b.kind === "code");
  await expect
    .poll(async () => {
      const doc = await stored();
      return code(doc).map((b) => doc.content.code_langs?.[b.id]);
    })
    .toEqual(["bash", "bash", "sql"]);
  const texts = code(await stored()).map((b) => b.text);
  for (const [i, block] of blocks.entries()) {
    await expect(highlighted(block)).toBeVisible();
    await block.getByRole("button", { name: "Copy code" }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(texts[i]);
  }
});

test.describe("accessibility", () => {
  test.use({ reducedMotion: "reduce" });

  for (const theme of ["light", "dark"] as const) {
    test(`the open language list and a chosen language have no axe violations in ${theme}`, async ({
      page,
    }) => {
      await chooseTheme(page, theme);
      await openLinkedPage(page);
      const goBlock = codeBlock(page, "func main()");
      await choose(page, goBlock, "Go");

      await picker(codeBlock(page, "SELECT id")).click();
      await expect(page.getByRole("listbox", { name: "Code language" })).toBeVisible();

      await expectNoAxeViolations(page);
    });
  }

  test.describe("at 375px", () => {
    test.use({ viewport: { width: 375, height: 800 } });

    test("the open language list fits the screen", async ({ page }) => {
      await openLinkedPage(page);
      await picker(codeBlock(page, "SELECT id")).click();
      const popup = page.locator(".code-lang-popup");
      await expect(popup).toBeVisible();

      await expectNoHorizontalScroll(page);
      const box = await popup.boundingBox();
      expect((box?.x ?? -1) >= 0 && (box?.x ?? 0) + (box?.width ?? 0) <= 375).toBe(true);
    });
  });
});

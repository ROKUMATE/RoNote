import type { Page } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
} from "./helpers";
import { stack } from "./stack";
import { expect, test } from "./test";

/** Imports fixture and waits until search finds it: indexing waits 5s for edits to settle. */
async function importIndexed(page: Page, fixture: string): Promise<string> {
  await page.goto("/");
  const path = await importPage(page, fixtureURL("pages", fixture));
  const id = path.replace("/doc/", "");
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`/api/v1/search?q=statements&document_id=${id}`);
        return ((await res.json()) as { results: unknown[] }).results.length;
      },
      { timeout: 30_000, message: `${fixture} indexed` },
    )
    .toBeGreaterThan(0);
  return path;
}

/** Opens the palette with the shortcut and searches for q. */
async function search(page: Page, q: string) {
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Search your library" });
  await palette.getByRole("combobox", { name: "Search your library" }).fill(q);
  return palette;
}

test("Mod+K finds a block of an imported page and lands on it", async ({ page }) => {
  const path = await importIndexed(page, "real-python-controlflow");
  await page.goto("/");

  const palette = await search(page, "for statements");
  const result = palette.getByRole("option").filter({ hasText: "4.2. for Statements" }).first();
  await expect(result).toBeVisible();
  // The flash lasts under a second: watch for it from before the click.
  const flashed = page.waitForSelector(".reader .landed");
  await result.click();

  await expect(page).toHaveURL(new RegExp(`${path}\\?block=`));
  const block = new URL(page.url()).searchParams.get("block") ?? "";
  expect(await (await flashed).getAttribute("data-block-id")).toBe(block);
  await expect(page.locator(`[data-block-id="${block}"]`)).toBeInViewport();
  await expect(palette).toBeHidden();
});

test("This document keeps the search to the open document", async ({ page }) => {
  const path = await importIndexed(page, "real-python-controlflow");
  await importIndexed(page, "real-go-effective-go");
  await page.goto(path);
  await expect(page.getByRole("article")).toBeVisible();

  const palette = await search(page, "statements");
  const groups = palette.getByRole("group");
  await expect(groups.filter({ hasText: "Effective Go" }).first()).toBeVisible();

  await palette.getByRole("checkbox", { name: "This document" }).check();

  await expect(groups.filter({ hasText: "Effective Go" })).toHaveCount(0);
  await expect(groups.first()).toHaveAttribute("aria-label", "4. More Control Flow Tools");
});

test("with the embedder down, the palette says the matches are words alone", async ({ page }) => {
  await importIndexed(page, "real-python-controlflow");
  await page.route("**/api/v1/search?**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({ url: `${stack.apiDegraded}${url.pathname}${url.search}` });
    await route.fulfill({ response });
  });
  await page.goto("/");

  const palette = await search(page, "for statements");

  await expect(
    palette.getByText("Search by meaning is unavailable right now, so these are word matches."),
  ).toBeVisible();
  await expect(palette.getByRole("option").first()).toBeVisible();
});

for (const theme of ["light", "dark"] as const) {
  test(`the palette passes axe in ${theme}, and fits 375px`, async ({ page }) => {
    await chooseTheme(page, theme);
    await importIndexed(page, "real-python-controlflow");
    await page.goto("/");

    const palette = await search(page, "for statements");
    await expect(palette.getByRole("option").first()).toBeVisible();
    await expectNoAxeViolations(page);

    await page.setViewportSize({ width: 375, height: 800 });
    await expect(palette.getByRole("option").first()).toBeVisible();
    await expectNoAxeViolations(page);
    await expectNoHorizontalScroll(page);
  });
}

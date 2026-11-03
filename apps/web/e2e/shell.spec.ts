import { expect, test } from "@playwright/test";
import { expectNoHorizontalScroll } from "./helpers";

test("renders the shell", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("banner")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  await expect(page.getByRole("main")).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Notes" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
});

test("the import button opens the import dialog", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });

  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("Page URL")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
});

test("shows a not found page for unknown paths", async ({ page }) => {
  await page.goto("/no-such-page");

  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible();
  await expect(page.getByRole("banner")).toBeVisible();
});

test.describe("at 375px", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("has no horizontal page scroll", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();

    await expectNoHorizontalScroll(page);
    await expect(page.getByRole("complementary", { name: "Notes" })).toBeHidden();
  });

  test("collapses the sidebar into a top bar", async ({ page }) => {
    await page.goto("/");
    const banner = await page.getByRole("banner").boundingBox();
    const main = await page.getByRole("main").boundingBox();

    expect(banner, "banner box").not.toBeNull();
    expect(main, "main box").not.toBeNull();
    if (!banner || !main) {
      return;
    }
    expect(banner.width, "top bar spans the viewport").toBe(375);
    expect(banner.height, "top bar is one row").toBeLessThan(100);
    expect(banner.y + banner.height, "top bar sits above main").toBeLessThanOrEqual(main.y);
  });
});

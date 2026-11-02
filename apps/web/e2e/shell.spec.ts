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

test("explains the disabled import button", async ({ page }) => {
  await page.goto("/");
  const button = page.getByRole("button", { name: "Import page" });
  const tooltip = page.getByRole("tooltip");

  await expect(button).toHaveAttribute("aria-disabled", "true");
  await expect(button).toHaveAccessibleDescription("Coming in P08");
  await expect(tooltip).toBeHidden();
  await button.focus();
  await expect(tooltip).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(tooltip).toBeHidden();
  await button.hover();
  await expect(tooltip).toBeVisible();
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

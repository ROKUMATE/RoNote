import { expect, test } from "@playwright/test";

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

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBe(0);
    await expect(page.getByRole("complementary", { name: "Notes" })).toBeHidden();
  });
});

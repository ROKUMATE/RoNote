import { expect, type Page, test } from "@playwright/test";

async function choose(page: Page, setting: string) {
  await page.getByRole("button", { name: "Settings" }).click();
  const dialog = page.getByRole("dialog", { name: "Settings" });
  await dialog.getByRole("radio", { name: setting }).check();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
}

async function expectChecked(page: Page, setting: string) {
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(
    page.getByRole("dialog", { name: "Settings" }).getByRole("radio", { name: setting }),
  ).toBeChecked();
}

test("the theme choice survives a reload", async ({ page }) => {
  await page.goto("/");
  await choose(page, "Dark");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  await page.reload();

  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator("html")).toHaveCSS("background-color", "rgb(23, 22, 19)");
  await expectChecked(page, "Dark");
});

test("the saved theme applies before the app loads", async ({ page }) => {
  await page.goto("/");
  await choose(page, "Dark");

  await page.route("**/src/main.tsx", (route) => route.abort());
  await page.reload();

  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("banner")).toHaveCount(0);
});

test("the reader font choice survives a reload", async ({ page }) => {
  await page.goto("/");
  await choose(page, "Sans");
  await expect(page.locator("html")).toHaveAttribute("data-reader-font", "sans");

  await page.reload();

  await expect(page.locator("html")).toHaveAttribute("data-reader-font", "sans");
  await expectChecked(page, "Sans");
});

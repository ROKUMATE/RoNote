import { expect, type Page, test } from "@playwright/test";
import { chooseTheme, expectNoAxeViolations, expectNoHorizontalScroll } from "./helpers";

async function openStyleguide(page: Page, theme: "light" | "dark") {
  await chooseTheme(page, theme);
  await page.goto("/_styleguide");
  await expect(page.getByRole("heading", { level: 1, name: "Styleguide" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
}

for (const theme of ["light", "dark"] as const) {
  test(`the styleguide has no axe violations in ${theme}`, async ({ page }) => {
    await openStyleguide(page, theme);

    await expectNoAxeViolations(page);
  });
}

test.describe("at 375px", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("the styleguide has no horizontal page scroll", async ({ page }) => {
    await openStyleguide(page, "light");
    await expectNoHorizontalScroll(page);
  });
});

test("code blocks scroll sideways instead of wrapping", async ({ page }) => {
  await openStyleguide(page, "light");
  const longLine = page.locator(".reader pre").nth(1);

  await expect(longLine).toHaveCSS("white-space", "pre");
  expect(await longLine.evaluate((pre) => pre.scrollWidth > pre.clientWidth)).toBe(true);
});

for (const { font, family } of [
  { font: "serif", family: /^"Source Serif 4 Variable"/ },
  { font: "sans", family: /^"IBM Plex Sans Variable"/ },
]) {
  test(`the ${font} reader font sets the reader column`, async ({ page }) => {
    await page.addInitScript((value) => localStorage.setItem("ronote.readerFont", value), font);
    await openStyleguide(page, "light");

    await expect(page.locator(".reader")).toHaveCSS("font-family", family);
  });
}

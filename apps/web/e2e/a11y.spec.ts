import { expect, test } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
  openLibrary,
  startImport,
} from "./helpers";

// adapter-fallback is extracted with the generic rules, so its document
// shows the warning notice.
const fixture = "adapter-fallback";

// axe measures contrast as it finds it; without motion the dialog is never
// caught halfway through fading in.
test.use({ reducedMotion: "reduce" });

for (const theme of ["light", "dark"] as const) {
  test(`the library, the import dialog and a document have no axe violations in ${theme}`, async ({
    page,
  }) => {
    await chooseTheme(page, theme);
    await page.goto("/");
    const url = fixtureURL("pages", fixture);

    await importPage(page, url);
    await expect(page.getByRole("note")).toBeVisible();
    await expectNoAxeViolations(page);

    await openLibrary(page);
    await expect(page.getByRole("main").getByRole("listitem").first()).toBeVisible();
    await expectNoAxeViolations(page);

    await page.getByRole("button", { name: "Import page" }).click();
    await expect(page.getByRole("dialog", { name: "Import page" })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.keyboard.press("Escape");

    const dialog = await startImport(page, fixtureURL("slow", fixture));
    await expect(dialog.getByRole("list", { name: "Progress" })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.keyboard.press("Escape");

    await startImport(page, url);
    await expect(page.getByText("Already in your library")).toBeVisible();
    await expectNoAxeViolations(page);
  });
}

test.describe("at 375px", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("the library, the import dialog and a document fit the screen", async ({ page }) => {
    await page.goto("/");

    await importPage(page, fixtureURL("pages", fixture));
    await expectNoHorizontalScroll(page);

    await openLibrary(page);
    await expectNoHorizontalScroll(page);

    await page.getByRole("button", { name: "Import page" }).click();
    await expect(page.getByRole("dialog", { name: "Import page" })).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
});

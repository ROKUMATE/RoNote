import { expect, type Page, test } from "@playwright/test";
import { fixtureURL, importPage } from "./helpers";

/** Imports linked page A, whose links lead to page B and a PDF. */
async function openLinkedPage(page: Page) {
  await page.goto("/");
  const path = await importPage(page, fixtureURL("linked", "linked-a"));
  await expect(page.getByRole("heading", { level: 1, name: "Linked guide A" })).toBeVisible();
  return path;
}

test("same-domain links show whether following them imports", async ({ page }) => {
  await openLinkedPage(page);

  const partB = page.getByRole("link", { name: "part B of the guide" });
  await expect(partB.locator('[data-link="import"]')).toBeVisible();
  await expect(partB).toHaveAttribute("aria-describedby", "link-follow-help");
  await expect(partB).not.toHaveAttribute("target", /.*/);
  const outside = page.getByRole("link", { name: "an outside page" });
  await expect(outside).toHaveAttribute("target", "_blank");
  await expect(outside).toHaveAttribute("rel", "noopener noreferrer");
  await expect(outside.locator("[data-link]")).toHaveCount(0);
});

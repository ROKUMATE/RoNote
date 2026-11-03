import { expect, test } from "@playwright/test";
import {
  expected,
  fixtureURL,
  importPage,
  importTimeout,
  openLibrary,
  startImport,
} from "./helpers";
import { stack } from "./stack";

declare global {
  interface Window {
    /** Each step the import dialog marked current, in the order it did. */
    importSteps: string[];
  }
}

test("imports a page, showing each step as it happens", async ({ page }) => {
  await page.addInitScript(() => {
    window.importSteps = [];
    new MutationObserver(() => {
      const current = document.querySelector('[data-step][data-state="current"]');
      const step = current?.getAttribute("data-step");
      if (step && window.importSteps.at(-1) !== step) window.importSteps.push(step);
    }).observe(document, { subtree: true, childList: true, attributeFilter: ["data-state"] });
  });
  const fixture = "docusaurus-v3-install";
  const { title, code } = expected(fixture);
  await page.goto("/");

  const path = await importPage(page, fixtureURL("slow", fixture));

  const steps = await page.evaluate(() => window.importSteps);
  expect(steps.slice(0, 2), "steps seen").toEqual(["queued", "fetching"]);
  const order = ["queued", "fetching", "extracting", "done"];
  expect(
    steps.map((s) => order.indexOf(s)),
    "steps in order",
  ).toEqual(steps.map((s) => order.indexOf(s)).toSorted());
  await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
  await expect(page.getByRole("list", { name: "Blocks" })).toContainText(`${code} code blocks`);
  // The sidebar was on screen all along: only the invalidation after done
  // brings the new document into it.
  await expect(
    page.getByRole("region", { name: "Recent" }).locator(`a[href="${path}"]`),
  ).toHaveText(title);
});

test("a page imported again opens the same document", async ({ page }) => {
  const url = fixtureURL("pages", "starlight-tabs");
  await page.goto("/");
  const first = await importPage(page, url);
  await openLibrary(page);

  await startImport(page, url);

  await expect(page.getByText("Already in your library")).toBeVisible();
  expect(new URL(page.url()).pathname).toBe(first);
});

test("a site whose robots.txt keeps RoNote out says so", async ({ page }) => {
  await page.goto("/");

  const dialog = await startImport(page, fixtureURL("private", "starlight-rag"));

  await expect(dialog.getByRole("alert")).toHaveText(
    "This site doesn't allow RoNote to fetch its pages.",
    { timeout: importTimeout },
  );
});

test("a page the site does not have says so", async ({ page }) => {
  await page.goto("/");

  const dialog = await startImport(page, `${stack.fixtures}/missing/?run=${crypto.randomUUID()}`);

  await expect(dialog.getByRole("alert")).toHaveText("The site says that page doesn't exist.", {
    timeout: importTimeout,
  });
});

test("the library still lists an imported page after a reload", async ({ page }) => {
  const fixture = "vitepress-vite-guide";
  await page.goto("/");
  const path = await importPage(page, fixtureURL("pages", fixture));
  await openLibrary(page);

  await page.reload();

  const link = page.getByRole("main").locator(`a[href="${path}"]`);
  await expect(link).toHaveText(expected(fixture).title);
});

test("closing the dialog leaves the import running in the library", async ({ page }) => {
  const fixture = "mdbook-v04-reading-files";
  const url = fixtureURL("slow", fixture);
  await page.goto("/");
  const dialog = await startImport(page, url);
  await expect(dialog.getByRole("list", { name: "Progress" })).toBeVisible();

  await dialog.getByRole("button", { name: "Close" }).click();

  const pending = page.getByRole("list", { name: "Documents" }).getByText(url);
  await expect(pending).toBeVisible();
  await expect(pending).toBeHidden({ timeout: importTimeout });
  await expect(
    page
      .getByRole("main")
      .getByRole("link", { name: expected(fixture).title })
      .first(),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/");
});

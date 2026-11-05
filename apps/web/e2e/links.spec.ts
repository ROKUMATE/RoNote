import { expect } from "@playwright/test";
import {
  caretHeading,
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  importTimeout,
  openLinkedPage,
  test,
} from "./helpers";

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

test("a link to a page not imported yet imports it, showing its steps, then opens it", async ({
  page,
}) => {
  await openLinkedPage(page);

  await page.getByRole("link", { name: "part B of the guide" }).click();

  await expect(
    page.getByRole("status").filter({ hasText: /Fetching the page|Queued/ }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Linked guide B" })).toBeVisible({
    timeout: importTimeout,
  });
});

test("going back returns to the same place, and the link then opens without importing", async ({
  page,
}) => {
  const pathA = await openLinkedPage(page);
  const link = page.getByRole("link", { name: "part B of the guide" });
  await link.scrollIntoViewIfNeeded();
  // Where the reader is: the link's place in the viewport. scrollY itself
  // differs on return, since blocks not yet shown keep placeholder heights.
  const top = async () =>
    (await page.getByRole("link", { name: "part B of the guide" }).boundingBox())?.y ?? -1;
  const before = await top();
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(300);
  await link.click();
  await expect(page.getByRole("heading", { level: 1, name: "Linked guide B" })).toBeVisible({
    timeout: importTimeout,
  });
  const pathB = new URL(page.url()).pathname;

  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`${pathA}$`));
  await expect(
    page.getByRole("link", { name: "part B of the guide" }).locator('[data-link="internal"]'),
  ).toBeVisible();
  await expect.poll(async () => Math.abs((await top()) - before)).toBeLessThan(5);
  const imports: string[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().endsWith("/api/v1/imports")) imports.push(r.url());
  });
  await page.getByRole("link", { name: "part B of the guide" }).click();

  await expect(page).toHaveURL(new RegExp(`${pathB}$`));
  expect(imports).toEqual([]);
});

test("a link to a section opens its page at that section, with the caret there", async ({
  page,
}) => {
  await openLinkedPage(page);

  await page.getByRole("link", { name: "its usage section" }).click();

  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}#usage$/, { timeout: importTimeout });
  const usage = page.getByRole("heading", { level: 2, name: "Usage" });
  await expect(usage).toBeInViewport();
  await expect.poll(() => caretHeading(page)).toBe("Usage");
});

test("going back holds the place when something above it arrives late", async ({ page }) => {
  await openLinkedPage(page);
  const top = async () =>
    (await page.getByRole("link", { name: "part B of the guide" }).boundingBox())?.y ?? -1;
  const link = page.getByRole("link", { name: "part B of the guide" });
  await link.scrollIntoViewIfNeeded();
  const before = await top();
  await link.click();
  await expect(page.getByRole("heading", { level: 1, name: "Linked guide B" })).toBeVisible({
    timeout: importTimeout,
  });
  // As on a slow runner, where part of the page came after the restore: the
  // header grows soon after page A is back, above where the reader was.
  // Chrome's scroll anchoring would hide the jump here, but Safari has none,
  // and Chrome's misses a change in the same frame as a scroll, as on CI:
  // without it, only the restore can put the reader back.
  await page.evaluate(() => {
    document.documentElement.style.overflowAnchor = "none";
    const watch = new MutationObserver(() => {
      const header = document.querySelector("article header");
      if (!header || document.querySelector(".reader h1")?.textContent !== "Linked guide A") return;
      watch.disconnect();
      setTimeout(() => {
        const late = document.createElement("div");
        late.style.height = "60px";
        header.append(late);
      }, 150);
    });
    watch.observe(document.body, { childList: true, subtree: true });
  });

  await page.goBack();

  await expect(page.locator("article header > div[style]")).toHaveCount(1);
  await expect.poll(async () => Math.abs((await top()) - before)).toBeLessThan(5);
});

test("Cmd-click opens the original page instead", async ({ page, context }) => {
  await openLinkedPage(page);
  const link = page.getByRole("link", { name: "part B of the guide" });
  const href = await link.getAttribute("href");

  const [original] = await Promise.all([
    context.waitForEvent("page"),
    link.click({ modifiers: ["ControlOrMeta"] }),
  ]);

  await expect.poll(() => original.url()).toBe(href);
  await expect(page.getByRole("heading", { level: 1, name: "Linked guide A" })).toBeVisible();
});

test("a link to a PDF says it cannot be imported, and offers the original", async ({ page }) => {
  await openLinkedPage(page);
  const link = page.getByRole("link", { name: "the PDF handout" });

  await link.click();

  const alert = page.getByRole("alert");
  await expect(alert).toContainText("That link isn't a web page. PDF import comes later.", {
    timeout: importTimeout,
  });
  await expect(alert.getByRole("link", { name: "Open original" })).toHaveAttribute(
    "href",
    (await link.getAttribute("href")) ?? "",
  );
});

test.describe("accessibility", () => {
  test.use({ reducedMotion: "reduce" });

  for (const theme of ["light", "dark"] as const) {
    test(`links and their progress have no axe violations in ${theme}`, async ({ page }) => {
      await chooseTheme(page, theme);
      await openLinkedPage(page);
      await expectNoAxeViolations(page);

      await page.getByRole("link", { name: "the PDF handout" }).click();
      await expect(page.getByRole("alert")).toBeVisible({ timeout: importTimeout });
      await expectNoAxeViolations(page);

      await page.getByRole("link", { name: "part B of the guide" }).click();
      await expect(
        page.getByRole("status").filter({ hasText: /Fetching the page|Queued/ }),
      ).toBeVisible();
      await expectNoAxeViolations(page);
    });
  }

  test.describe("at 375px", () => {
    test.use({ viewport: { width: 375, height: 800 } });

    test("a failed link import fits the screen", async ({ page }) => {
      await openLinkedPage(page);
      await page.getByRole("link", { name: "the PDF handout" }).click();
      await expect(page.getByRole("alert")).toBeVisible({ timeout: importTimeout });

      await expectNoHorizontalScroll(page);
    });
  });
});

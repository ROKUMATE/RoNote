import type { Page } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
} from "./helpers";
import { expect, test } from "./test";

/** Imports the JavaScript fixture page and waits for its code blocks' controls. */
async function openJavaScriptPage(page: Page) {
  await page.goto("/");
  await importPage(page, fixtureURL("linked", "javascript"));
  await expect(
    page.getByRole("heading", { level: 1, name: "JavaScript in the browser" }),
  ).toBeVisible();
  const blocks = page.locator(".code-block");
  await expect(blocks).toHaveCount(6);
  await expect(blocks.first().getByRole("button", { name: "Run" })).toBeVisible();
  return blocks;
}

test("blocks share one JavaScript session, and Restart clears it", async ({ page }) => {
  const blocks = await openJavaScriptPage(page);
  const [define, use] = [blocks.nth(0), blocks.nth(1)];
  await expect(define.getByRole("button", { name: "JavaScript · browser" })).toBeVisible();

  await define.getByRole("button", { name: "Run" }).click();
  await expect(define.getByRole("status")).toHaveText(/^Ran in /);
  await use.getByRole("button", { name: "Run" }).click();
  await expect(use.locator(".run-result")).toHaveText("value'Hello, JavaScript'");

  await use.getByRole("button", { name: "Restart" }).click();
  await expect(use.getByText("JavaScript restarted. Earlier results are gone.")).toBeVisible();
  await use.getByRole("button", { name: "Run" }).click();
  await expect(use.getByRole("status")).toHaveText(/^Ended with an error after /);
  await expect(use.locator(".run-error")).toContainText(
    "Uncaught ReferenceError: greeting is not defined",
  );
});

test("the header shows the session as it goes, and its Restart clears it", async ({ page }) => {
  const blocks = await openJavaScriptPage(page);
  const sessions = page.getByRole("list", { name: "Code sessions" });
  await expect(sessions.getByRole("listitem")).toHaveText(["JavaScript: not startedRestart"]);
  await expect(sessions.getByRole("button", { name: "Restart JavaScript" })).toBeDisabled();

  await blocks.nth(2).getByRole("button", { name: "Run" }).click();
  await expect(sessions.getByRole("listitem")).toHaveText(["JavaScript: runningRestart"]);
  await blocks.nth(2).getByRole("button", { name: "Stop" }).click();
  await expect(sessions.getByRole("listitem")).toHaveText(["JavaScript: not startedRestart"]);

  await blocks.nth(0).getByRole("button", { name: "Run" }).click();
  await expect(sessions.getByRole("listitem")).toHaveText(["JavaScript: idleRestart"]);
  await sessions.getByRole("button", { name: "Restart JavaScript" }).click();
  await expect(sessions.getByRole("listitem")).toHaveText(["JavaScript: not startedRestart"]);
  await blocks.nth(1).getByRole("button", { name: "Run" }).click();
  await expect(blocks.nth(1).locator(".run-error")).toContainText("greeting is not defined");
});

test("Stop ends an endless JavaScript loop within a second", async ({ page }) => {
  const blocks = await openJavaScriptPage(page);
  const loop = blocks.nth(2);
  await loop.getByRole("button", { name: "Run" }).click();
  await expect(loop.getByRole("status")).toHaveText("Running…");

  await page.waitForTimeout(300);
  const stopped = Date.now();
  await loop.getByRole("button", { name: "Stop" }).click();
  await expect(loop.getByRole("status")).toHaveText(/^Stopped after /);
  expect(Date.now() - stopped).toBeLessThan(1000);
  await expect(
    loop.getByText("Stopping restarted JavaScript. Earlier results are gone."),
  ).toBeVisible();
  await expect(loop.getByRole("button", { name: "Run" })).toBeVisible();
});

test("an unhandled rejection is shown", async ({ page }) => {
  const blocks = await openJavaScriptPage(page);
  const rejecting = blocks.nth(3);
  await rejecting.getByRole("button", { name: "Run" }).click();
  await expect(rejecting.getByRole("status")).toHaveText(/^Ended with an error after /);
  await expect(rejecting.locator(".run-error")).toContainText(
    "Uncaught (in promise) Error: nobody caught this",
  );
});

test("MDN's examples: the DOM block says why it can't run, and the pure one runs", async ({
  page,
}) => {
  const blocks = await openJavaScriptPage(page);
  const dom = blocks.nth(4);
  await expect(dom.getByRole("button", { name: /^Run/ })).toHaveCount(0);
  await dom.getByRole("button", { name: "Needs a display" }).focus();
  await expect(dom.getByRole("tooltip")).toHaveText("It needs a window or a display.");

  const tally = blocks.nth(5);
  await tally.getByRole("button", { name: "Run" }).click();
  await expect(tally.locator(".run-result")).toHaveText("value2");
});

for (const theme of ["light", "dark"] as const) {
  test(`JavaScript output has no axe violations in ${theme}, and fits 375px`, async ({ page }) => {
    await chooseTheme(page, theme);
    await page.emulateMedia({ reducedMotion: "reduce" });
    const blocks = await openJavaScriptPage(page);
    await blocks.nth(3).getByRole("button", { name: "Run" }).click();
    await expect(blocks.nth(3).locator(".run-error")).toBeVisible();
    await blocks.nth(5).getByRole("button", { name: "Run" }).click();
    await expect(blocks.nth(5).locator(".run-result")).toBeVisible();

    await expectNoAxeViolations(page);
    await page.setViewportSize({ width: 375, height: 800 });
    await expectNoHorizontalScroll(page);
  });
}

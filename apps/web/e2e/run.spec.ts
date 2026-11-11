import type { Page } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
} from "./helpers";
import { expect, test } from "./test";

/** Imports the Python fixture page and waits for its code blocks' controls. */
async function openPythonPage(page: Page) {
  await page.goto("/");
  await importPage(page, fixtureURL("linked", "python"));
  await expect(
    page.getByRole("heading", { level: 1, name: "Python in the browser" }),
  ).toBeVisible();
  const blocks = page.locator(".code-block");
  await expect(blocks).toHaveCount(8);
  await expect(blocks.first().getByRole("button", { name: "Run" })).toBeVisible();
  return blocks;
}

test("blocks share one session, and Restart clears it", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const [define, use] = [blocks.nth(0), blocks.nth(1)];

  await define.getByRole("button", { name: "Run" }).click();
  await expect(define.getByRole("status")).toHaveText(/^Ran in /, { timeout: 20_000 });
  await use.getByRole("button", { name: "Run" }).click();
  await expect(use.getByRole("status")).toHaveText(/^Ran in /);
  await expect(use.locator(".run-text")).toHaveText("42\n");

  await use.getByRole("button", { name: "Restart" }).click();
  await expect(use.getByText("Python restarted. Earlier results are gone.")).toBeVisible();
  await use.getByRole("button", { name: "Run" }).click();
  await expect(use.getByRole("status")).toHaveText(/^Ended with an error after /, {
    timeout: 20_000,
  });
  await expect(use.locator(".run-error")).toContainText("NameError: name 'x' is not defined");
});

test("Stop ends an endless loop within a second", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const loop = blocks.nth(2);
  await loop.getByRole("button", { name: "Run" }).click();
  await expect(loop.getByRole("status")).toHaveText("Running…", { timeout: 20_000 });

  await page.waitForTimeout(300);
  const stopped = Date.now();
  await loop.getByRole("button", { name: "Stop" }).click();
  await expect(loop.getByRole("status")).toHaveText(/^Stopped after /);
  expect(Date.now() - stopped).toBeLessThan(1000);
  await expect(loop.getByRole("button", { name: "Run" })).toBeVisible();
});

test("output stops at 1 MB, and the page stays responsive", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const flood = blocks.nth(3);
  await flood.getByRole("button", { name: "Run" }).click();
  await expect(flood.getByRole("status")).toHaveText("Stopped: the output passed 1 MB", {
    timeout: 20_000,
  });
  expect(await flood.locator(".run-text").evaluate((pre) => pre.textContent?.length)).toBe(
    1_000_000,
  );

  const roundTrip = await page.evaluate(
    () =>
      new Promise<number>((resolve) => {
        const started = performance.now();
        setTimeout(() => resolve(performance.now() - started));
      }),
  );
  expect(roundTrip).toBeLessThan(250);
  await flood.getByRole("button", { name: "Clear" }).click();
  await expect(flood.locator(".run-output")).toHaveCount(0);
});

test("blocks that can't run say why, and outputs are never saved", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const network = blocks.nth(4);
  await expect(network.getByRole("button", { name: /^Run/ })).toHaveCount(0);
  const chip = network.getByRole("button", { name: "Needs network" });
  await chip.focus();
  await expect(network.getByRole("tooltip")).toHaveText(
    "It reaches the network, which code here can't.",
  );
  await page.keyboard.press("Escape");
  await expect(network.getByRole("tooltip")).toBeHidden();
  await expect(blocks.nth(5).getByRole("button", { name: "Shell" })).toBeVisible();

  await blocks.nth(0).getByRole("button", { name: "Run" }).click();
  await expect(blocks.nth(0).getByRole("status")).toHaveText(/^Ran in /, { timeout: 20_000 });
  await page.reload();
  await expect(blocks.first().getByRole("button", { name: "Run" })).toBeVisible();
  await expect(page.locator(".run-output")).toHaveCount(0);
});

test("Check examples finds the one example whose output differs, with its diff", async ({
  page,
}) => {
  const blocks = await openPythonPage(page);
  const transcript = blocks.nth(6);
  await transcript.getByRole("button", { name: "Check examples" }).click();
  await expect(transcript.getByRole("status")).toHaveText(
    /^2 of 3 examples passed, 1 failed, in /,
    { timeout: 20_000 },
  );
  const examples = transcript.getByRole("list", { name: "Examples" }).getByRole("listitem");
  await expect(examples.locator(".run-verdict")).toHaveText(["Passed", "Passed", "Failed"]);
  await expect(examples.nth(2).locator("code")).toHaveText('>>> len("abc")');
  await expect(examples.nth(2).locator(".run-diff")).toHaveText("- 4\n+ 3\n");
  await expect(transcript.locator(".run-diff")).toHaveCount(1);
});

test("Mod+Enter runs the code block the reader is in", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const define = blocks.nth(0);
  await define.locator("pre").click();
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(define.getByRole("status")).toHaveText(/^Ran in /, { timeout: 20_000 });

  // From the block's own chip too; the editor never takes the Enter.
  const topLevel = page.locator(".reader-content > *");
  const before = await topLevel.count();
  const use = blocks.nth(1);
  await use.getByRole("button", { name: "Python · browser" }).focus();
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(use.locator(".run-text")).toHaveText("42\n");
  expect(await topLevel.count()).toBe(before);
});

test("a block with no language asks for one, and runs once Python is chosen", async ({ page }) => {
  const blocks = await openPythonPage(page);
  const bare = blocks.nth(7);
  await expect(bare.getByRole("button", { name: /^Run/ })).toHaveCount(0);
  const chip = bare.getByRole("button", { name: "No language" });
  await chip.focus();
  await expect(bare.getByRole("tooltip")).toHaveText(
    "This block has no language set. Choose one to run it.",
  );

  await chip.click();
  const list = bare.getByRole("listbox", { name: "Code language" });
  await expect(list).toBeFocused();
  await list.getByRole("option", { name: "Python" }).click();
  await bare.getByRole("button", { name: "Run" }).click();
  await expect(bare.getByRole("status")).toHaveText(/^Ran in /, { timeout: 20_000 });
  await expect(bare.locator(".run-text")).toHaveText("chosen as Python\n");
});

for (const theme of ["light", "dark"] as const) {
  test(`the output panel has no axe violations in ${theme}, and fits 375px`, async ({ page }) => {
    await chooseTheme(page, theme);
    await page.emulateMedia({ reducedMotion: "reduce" });
    const blocks = await openPythonPage(page);
    const use = blocks.nth(1);
    await use.getByRole("button", { name: "Run" }).click();
    await expect(use.getByRole("status")).toHaveText(/^Ended with an error/, { timeout: 20_000 });
    const transcript = blocks.nth(6);
    await transcript.getByRole("button", { name: "Check examples" }).click();
    await expect(transcript.locator(".run-diff")).toBeVisible({ timeout: 20_000 });
    await blocks.nth(4).getByRole("button", { name: "Needs network" }).focus();

    await expectNoAxeViolations(page);
    await page.setViewportSize({ width: 375, height: 800 });
    await expectNoHorizontalScroll(page);
  });
}

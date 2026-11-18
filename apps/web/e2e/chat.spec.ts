import type { Locator, Page } from "@playwright/test";
import {
  chooseTheme,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  importIndexed,
} from "./helpers";
import { stack } from "./stack";
import { expect, ownUserTest as test } from "./test";

// The fake model (cmd/fakellm) cites the first block it is sent holding one
// of the question's longer words, and calls not_in_library for a question
// none of them is in. "no sources" gets an answer that cites nothing. Each test asks about one document, so other
// tests' imports in the worker's library never answer it.

// axe measures contrast as it finds it; without motion nothing on the page
// is caught halfway through fading in.
test.use({ reducedMotion: "reduce" });

/** Imports the Python control flow page and opens the chat over it. */
async function chatAboutControlFlow(page: Page): Promise<{ path: string; panel: Locator }> {
  const path = await importIndexed(page, "real-python-controlflow");
  await expect(page.getByRole("article")).toBeVisible();
  await page.keyboard.press("ControlOrMeta+j");
  const panel = page.getByRole("dialog", { name: "Chat" });
  await expect(panel.getByRole("radio", { name: "This document" })).toBeChecked();
  return { path, panel };
}

async function ask(panel: Locator, question: string) {
  const box = panel.getByRole("textbox", { name: "Ask your library" });
  await box.fill(question);
  await box.press("Enter");
}

test("Mod+J asks about a document: the answer streams with a citation that lands on its block", async ({
  page,
}) => {
  const { path, panel } = await chatAboutControlFlow(page);
  await ask(panel, "How do for statements iterate?");

  const chip = panel.getByRole("button", { name: /^Source 1: / });
  await expect(chip).toBeVisible();
  await expect(panel.getByText("generated", { exact: true }).first()).toBeVisible();
  await expect(panel.getByText(/questions left today/)).toBeVisible();

  // Hovering the chip shows what it cites.
  await chip.hover();
  const cited = await page.evaluate(
    (id) => document.getElementById(id ?? "")?.textContent ?? "",
    await chip.getAttribute("aria-describedby"),
  );
  expect(cited).toContain("statement");
  await expect(panel.getByText(cited.slice(0, 40)).last()).toBeVisible();

  // The flash lasts under a second: watch for it from before the click.
  const flashed = page.waitForSelector(".reader .landed");
  await chip.click();
  await expect(page).toHaveURL(new RegExp(`${path}\\?block=`));
  const block = new URL(page.url()).searchParams.get("block") ?? "";
  expect(await (await flashed).getAttribute("data-block-id")).toBe(block);
  await expect(page.locator(`[data-block-id="${block}"]`)).toBeInViewport();
});

test("a question the document doesn't answer is not found in the library", async ({ page }) => {
  const { panel } = await chatAboutControlFlow(page);
  await ask(panel, "How do I configure ServeMux routes?");
  await expect(panel.getByText("Not found in your library")).toBeVisible();
  await expect(panel.getByText("Your library doesn't cover this.")).toBeVisible();
  await expect(
    panel.getByText("To find it, import a page about configure and servemux."),
  ).toBeVisible();
  await expect(panel.getByRole("button", { name: /^Source/ })).toHaveCount(0);
});

test("an answer that cites nothing says it has no sources", async ({ page }) => {
  const { panel } = await chatAboutControlFlow(page);
  await ask(panel, "How do for statements iterate, with no sources?");
  await expect(panel.getByText("No sources", { exact: true })).toBeVisible();
  await expect(
    panel.getByText("This answer couldn't be checked against your library."),
  ).toBeVisible();
  await expect(panel.getByText("Goroutines are cheap to start.")).toBeVisible();
  await expect(panel.getByText("Not found in your library")).toHaveCount(0);
  await expect(panel.getByRole("button", { name: /^Source/ })).toHaveCount(0);
});

test("Stop ends an answer, which is saved as stopped", async ({ page }) => {
  const { panel } = await chatAboutControlFlow(page);
  // "slowly" makes the fake model stream a word at a time.
  await ask(panel, "Explain slowly how for statements work");
  await expect(panel.getByText(/^From /)).toBeVisible();
  await panel.getByRole("button", { name: "Stop" }).click();
  await expect(panel.getByText("Stopped.")).toBeVisible();
  await expect(panel.getByRole("button", { name: "Ask" })).toBeVisible();

  // Saved: the conversation reads back the same.
  await panel.getByRole("button", { name: "Conversations" }).click();
  await panel
    .getByRole("list", { name: "Conversations" })
    .getByRole("button", { name: /^Explain slowly/ })
    .first()
    .click();
  await expect(panel.getByText("Stopped.")).toBeVisible();
});

test("past the day's questions, the chat says so", async ({ page }) => {
  const { panel } = await chatAboutControlFlow(page);
  await ask(panel, "How do for statements iterate?");
  await expect(panel.getByRole("button", { name: /^Source 1: / })).toBeVisible();

  // From here on, an API that allows one question a day answers the chat.
  await page.route("**/api/v1/conversations/**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({
      url: `${stack.apiOneQuestion}${url.pathname}${url.search}`,
    });
    await route.fulfill({ response });
  });
  await ask(panel, "And while statements?");
  await expect(panel.getByRole("alert")).toHaveText(/daily limit/);
  await expect(panel.getByRole("textbox", { name: "Ask your library" })).toHaveValue(
    "And while statements?",
  );
});

for (const theme of ["light", "dark"] as const) {
  test(`the not found and no sources answers pass axe in ${theme}`, async ({ page }) => {
    await chooseTheme(page, theme);
    const { panel } = await chatAboutControlFlow(page);
    await ask(panel, "How do I configure ServeMux routes?");
    await expect(panel.getByText("Not found in your library")).toBeVisible();
    await ask(panel, "How do for statements iterate, with no sources?");
    await expect(panel.getByText("No sources", { exact: true })).toBeVisible();
    await expect(panel.getByRole("button", { name: "Ask" })).toBeVisible();
    await expectNoAxeViolations(page);
  });

  test(`the chat passes axe in ${theme}, and is a full sheet at 375px`, async ({ page }) => {
    await chooseTheme(page, theme);
    // Over the library: the document pages' own contrast is the reader's.
    await importIndexed(page, "real-python-controlflow");
    await page.goto("/");
    await page.keyboard.press("ControlOrMeta+j");
    const panel = page.getByRole("dialog", { name: "Chat" });
    await ask(panel, "How do for statements iterate?");
    // Once saved, the answer's chip shows what it cites while focused.
    await expect(panel.getByRole("button", { name: "Ask" })).toBeVisible();
    await panel.getByRole("button", { name: /^Source 1: / }).focus();
    await expectNoAxeViolations(page);

    await page.keyboard.press("Escape");
    await expect(panel).toBeHidden();
    await page.setViewportSize({ width: 375, height: 800 });
    await page.keyboard.press("ControlOrMeta+j");
    await expect(panel).toBeVisible();
    const box = await panel.boundingBox();
    expect(box?.width).toBe(375);
    expect(box?.height).toBe(800);
    await expect(panel.getByRole("textbox", { name: "Ask your library" })).toBeVisible();
    await expectNoAxeViolations(page);
    await expectNoHorizontalScroll(page);
  });
}

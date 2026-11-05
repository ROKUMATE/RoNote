import { expect, type Frame, type Page, test } from "@playwright/test";
import { stack } from "./stack";

declare global {
  interface Window {
    /** The events the runner sent the app, in order. */
    runnerEvents: unknown[];
  }
}

/**
 * Opens the library with a runner in it, through the app's own client, and
 * returns the runner's frame and whether it is isolated.
 */
async function openRunner(page: Page): Promise<{ frame: Frame; isolated: boolean }> {
  await page.goto("/");
  // A path, not a module specifier: Vite's dev server serves the source.
  const client = "/src/document/run/client.ts";
  const isolated = await page.evaluate(async (client) => {
    const { openRunner } = await import(client);
    const runner = await openRunner();
    window.runnerEvents = [];
    runner.listen((event: unknown) => window.runnerEvents.push(event));
    return runner.isolated as boolean;
  }, client);
  const frame = page.frames().find((f) => f.url().startsWith(stack.runner));
  if (!frame) throw new Error("no runner frame");
  return { frame, isolated };
}

test("the app and its runner are cross-origin isolated", async ({ page }) => {
  const { isolated } = await openRunner(page);
  expect(isolated).toBe(true);
  expect(await page.evaluate(() => crossOriginIsolated)).toBe(true);
});

test("the runner answers the app, and no other origin or window", async ({ page }) => {
  const { frame } = await openRunner(page);
  const hello = (origin: string, source: "parent" | "self") =>
    frame.evaluate(
      ([origin, source]) =>
        window.dispatchEvent(
          new MessageEvent("message", {
            data: { v: 1, type: "hello" },
            origin,
            source: source === "parent" ? window.parent : window,
          }),
        ),
      [origin, source] as const,
    );
  await hello(stack.fixtures, "parent");
  await hello(stack.runner, "parent");
  await hello(stack.web, "self");
  // Answers arrive in order, so once this one is in, any to the others would be.
  await hello(stack.web, "parent");

  await expect.poll(() => page.evaluate(() => window.runnerEvents.length)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.runnerEvents)).toEqual([
    { v: 1, type: "hello", isolated: true },
  ]);
});

test("code in the runner can't reach the API, directly or through the app", async ({ page }) => {
  const { frame } = await openRunner(page);
  const reach = (url: string) =>
    frame.evaluate(
      (url) =>
        fetch(url, { mode: "no-cors" }).then(
          () => "reached",
          () => "refused",
        ),
      url,
    );

  expect(await reach(`${stack.api}/readyz`)).toBe("refused");
  expect(await reach(`${stack.web}/api/v1/documents`)).toBe("refused");
  expect(await reach(`${stack.runner}/`), "its own origin").toBe("reached");
});

test("the runner can't read the app's cookies or storage", async ({ page }) => {
  const { frame } = await openRunner(page);
  await page.context().addCookies([{ name: "ronote_secret", value: "1", url: stack.web }]);
  await page.evaluate(() => {
    localStorage.setItem("ronote_secret", "1");
    sessionStorage.setItem("ronote_secret", "1");
  });

  const seen = await frame.evaluate(() => {
    const read = (value: () => unknown) => {
      try {
        return String(value());
      } catch {
        return "refused";
      }
    };
    return {
      cookies: document.cookie,
      local: localStorage.getItem("ronote_secret"),
      session: sessionStorage.getItem("ronote_secret"),
      parentCookies: read(() => window.parent.document.cookie),
      parentLocal: read(() => window.parent.localStorage.getItem("ronote_secret")),
    };
  });
  expect(seen).toEqual({
    cookies: "",
    local: null,
    session: null,
    parentCookies: "refused",
    parentLocal: "refused",
  });
});

test("only the app may frame the runner", async ({ page }) => {
  await page.goto(`${stack.fixtures}/robots.txt`);
  await page.evaluate((src) => {
    const frame = document.createElement("iframe");
    frame.src = src;
    document.body.append(frame);
  }, `${stack.runner}/`);

  await expect.poll(() => page.frames().length).toBe(2);
  const frame = page.frames()[1];
  await expect.poll(() => frame?.url()).not.toBe("about:blank");
  expect(frame?.url()).not.toContain(stack.runner);
});

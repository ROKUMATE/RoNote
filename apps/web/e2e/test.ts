import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { test as base, expect, type Page } from "@playwright/test";
import users from "./fixtures/idp-users.json" with { type: "json" };
import { stack } from "./stack";

export { expect };

/** The fake GitHub's users for one worker each, as idp-users.json lists them. */
const workerUsers = users.filter((u) => u.login.startsWith("e2e-w")).length;

/**
 * Signs page in as login through the login page and the fake GitHub, as a
 * person does, and waits to be back in the app.
 */
export async function signIn(page: Page, login: string) {
  if (!new URL(page.url()).pathname.startsWith("/login")) {
    await page.goto("/login");
  }
  await page.getByRole("link", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: `Sign in as ${login}`, exact: true }).click();
  await expect(page).not.toHaveURL(/\/login|127\.0\.0\.1:18091/);
}

/** The fake GitHub login of a worker's own user. */
export function workerLogin(): string {
  return `e2e-w${test.info().parallelIndex}`;
}

/**
 * The end-to-end tests' test: every page starts signed in as its worker's
 * own user (e2e-w0, e2e-w1, ...), whose library no other worker touches.
 * The worker signs in once, through the fake provider, and every test
 * reuses that browser state.
 */
export const test = base.extend<object, { workerStorageState: string }>({
  storageState: ({ workerStorageState }, use) => use(workerStorageState),
  workerStorageState: [
    async ({ browser }, use, workerInfo) => {
      if (workerInfo.parallelIndex >= workerUsers) {
        throw new Error(
          `worker ${workerInfo.parallelIndex} has no user: idp-users.json has ${workerUsers}; run fewer workers`,
        );
      }
      const dir = join(workerInfo.project.outputDir, ".auth");
      mkdirSync(dir, { recursive: true });
      const file = join(dir, `${workerInfo.parallelIndex}.json`);
      const page = await browser.newPage({ baseURL: stack.web });
      await signIn(page, `e2e-w${workerInfo.parallelIndex}`);
      await page.context().storageState({ path: file });
      await page.close();
      await use(file);
    },
    { scope: "worker" },
  ],
});

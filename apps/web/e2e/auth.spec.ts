import type { Browser, Page } from "@playwright/test";
import {
  chooseTheme,
  expected,
  expectNoAxeViolations,
  expectNoHorizontalScroll,
  fixtureURL,
  importPage,
} from "./helpers";
import { stack } from "./stack";
import { expect, signIn, test, workerLogin } from "./test";

/**
 * A page in a browser of its own, signed in as nobody. Contexts a test makes
 * take its options, the worker's session among them, unless told otherwise.
 */
async function signedOutPage(browser: Browser): Promise<Page> {
  const context = await browser.newContext({
    baseURL: stack.web,
    storageState: { cookies: [], origins: [] },
  });
  return context.newPage();
}

/** Signs a fresh browser in as login, and returns it at the library. */
async function freshSignIn(browser: Browser, login: string): Promise<Page> {
  const page = await signedOutPage(browser);
  await signIn(page, login);
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
  return page;
}

test.describe("signed out", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("signing in lands in the library", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login\?return_to=%2F$/);

    await signIn(page, workerLogin());

    await expect(page).toHaveURL(`${stack.web}/`);
    await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
    await expect(
      page.getByRole("button", { name: `Worker ${test.info().parallelIndex}` }),
    ).toBeVisible();
  });

  test("Deny at GitHub says so", async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("link", { name: "Continue with GitHub" }).click();

    await page.getByRole("button", { name: "Deny" }).click();

    await expect(page).toHaveURL(/\/login\?error=denied/);
    await expect(page.getByRole("alert")).toHaveText("GitHub didn't sign you in. Try again.");
  });

  test("an account with no verified email can't sign in", async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("link", { name: "Continue with GitHub" }).click();

    await page.getByRole("button", { name: "Sign in as e2e-unverified" }).click();

    await expect(page.getByRole("alert")).toHaveText(/needs a verified primary email/);
  });
});

test("a deep link while signed out comes back after signing in", async ({ page, browser }) => {
  await page.goto("/");
  const path = await importPage(page, fixtureURL("pages", "godev-getting-started"));
  const { title } = expected("godev-getting-started");
  const elsewhere = await signedOutPage(browser);

  await elsewhere.goto(path);
  await expect(elsewhere).toHaveURL(new RegExp(`/login\\?return_to=${encodeURIComponent(path)}$`));
  await signIn(elsewhere, workerLogin());

  await expect(elsewhere).toHaveURL(`${stack.web}${path}`);
  // The page's own title repeats as a heading in its content.
  await expect(elsewhere.getByRole("heading", { level: 1, name: title }).first()).toBeVisible();
});

test("two users can't see each other's documents", async ({ page, browser }) => {
  await page.goto("/");
  const path = await importPage(page, fixtureURL("pages", "godev-getting-started"));
  const { title } = expected("godev-getting-started");
  const id = path.replace("/doc/", "");
  const other = await freshSignIn(browser, "e2e-other");

  await expect(other.getByRole("link", { name: title })).toHaveCount(0);
  await other.goto(path);
  await expect(other.getByRole("heading", { level: 1, name: title })).toHaveCount(0);
  for (const target of [`/api/v1/documents/${id}`, `/api/v1/documents/${id}/annotations`]) {
    expect((await other.request.get(target)).status(), target).toBe(404);
  }
  expect((await page.request.get(`/api/v1/documents/${id}`)).status()).toBe(200);
});

test("signing out ends the session", async ({ browser }) => {
  const page = await freshSignIn(browser, workerLogin());
  await page.getByRole("button", { name: `Worker ${test.info().parallelIndex}` }).click();

  await page.getByRole("button", { name: "Sign out", exact: true }).click();

  await expect(page).toHaveURL(`${stack.web}/login`);
  expect((await page.request.get("/api/v1/me")).status()).toBe(401);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login\?return_to=%2F$/);
});

test("a session that ends mid-use goes to sign in and comes back", async ({ browser }) => {
  const page = await freshSignIn(browser, "e2e-expiry");
  // Another browser signs the same user out everywhere.
  const elsewhere = await freshSignIn(browser, "e2e-expiry");
  expect((await elsewhere.request.post("/api/v1/auth/logout-all")).status()).toBe(204);

  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByLabel("Page URL").fill(fixtureURL("pages", "godev-getting-started"));
  await dialog.getByRole("button", { name: "Import", exact: true }).click();

  await expect(page).toHaveURL(/\/login\?return_to=%2F$/);
  await signIn(page, "e2e-expiry");
  await expect(page).toHaveURL(`${stack.web}/`);
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
});

test("deleting the account leaves an empty library to sign in to", async ({ browser }) => {
  const page = await freshSignIn(browser, "e2e-deleter");
  await importPage(page, fixtureURL("pages", "godev-getting-started"));
  await page.getByRole("button", { name: "Deleter" }).click();
  await page.getByRole("button", { name: "Delete account…" }).click();
  const dialog = page.getByRole("dialog", { name: "Delete your account?" });
  await dialog.getByLabel(/Type your GitHub login/).fill("e2e-deleter");

  await dialog.getByRole("button", { name: "Delete account" }).click();

  await expect(page).toHaveURL(`${stack.web}/login`);
  await signIn(page, "e2e-deleter");
  await expect(page.getByText("No pages yet.")).toBeVisible();
});

for (const theme of ["light", "dark"] as const) {
  test.describe(`at 375px, ${theme}`, () => {
    test.use({ viewport: { width: 375, height: 800 } });

    test.describe("signed out", () => {
      test.use({ storageState: { cookies: [], origins: [] } });

      test("the login page passes axe", async ({ page }) => {
        await chooseTheme(page, theme);
        await page.goto("/login?error=state_mismatch");
        await expect(page.getByRole("alert")).toBeVisible();

        await expectNoAxeViolations(page);
        await expectNoHorizontalScroll(page);
      });
    });

    test("the delete dialog passes axe", async ({ page }) => {
      await chooseTheme(page, theme);
      await page.goto("/");
      await page.getByRole("button", { name: `Worker ${test.info().parallelIndex}` }).click();
      await page.getByRole("button", { name: "Delete account…" }).click();
      await expect(page.getByRole("dialog", { name: "Delete your account?" })).toBeVisible();

      await expectNoAxeViolations(page);
      await expectNoHorizontalScroll(page);
    });
  });
}

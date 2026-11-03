import { readFileSync } from "node:fs";
import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";
import { stack } from "./stack";

/** Fails if the page is wider than the viewport. */
export async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, "page scrolls sideways").toBe(0);
}

/** Fails on any axe violation, naming each rule and where it failed. */
export async function expectNoAxeViolations(page: Page) {
  const { violations } = await new AxeBuilder({ page }).analyze();
  expect(
    violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`),
  ).toEqual([]);
}

/** Saves the theme before the app loads, so it applies from the first paint. */
export async function chooseTheme(page: Page, theme: "light" | "dark") {
  await page.addInitScript((value) => localStorage.setItem("ronote.theme", value), theme);
}

/**
 * A fixture page's URL on the fixture site. Each call gives a URL no test
 * has imported yet, so tests do not depend on each other or on earlier runs.
 */
export function fixtureURL(kind: "pages" | "slow" | "private", fixture: string) {
  return `${stack.fixtures}/${kind}/${fixture}/?run=${crypto.randomUUID()}`;
}

type Block = { kind: string; children?: Block[]; items?: { children: Block[] }[] };

/** What the extractor makes of a golden fixture: its title and code blocks. */
export function expected(fixture: string): { title: string; code: number } {
  const doc: { title: string; blocks: Block[] } = JSON.parse(
    readFileSync(
      new URL(`../../../testdata/golden/${fixture}/expected.json`, import.meta.url),
      "utf8",
    ),
  );
  const count = (blocks: Block[]): number =>
    blocks.reduce(
      (n, b) =>
        n +
        (b.kind === "code" ? 1 : 0) +
        count(b.children ?? []) +
        count((b.items ?? []).flatMap((item) => item.children)),
      0,
    );
  return { title: doc.title, code: count(doc.blocks) };
}

/** Opens the import dialog from the library and submits url. */
export async function startImport(page: Page, url: string) {
  await page.getByRole("button", { name: "Import page" }).click();
  const dialog = page.getByRole("dialog", { name: "Import page" });
  await dialog.getByLabel("Page URL").fill(url);
  await dialog.getByRole("button", { name: "Import", exact: true }).click();
  return dialog;
}

/** How long an import may take: the fetcher waits a second between requests to one host. */
export const importTimeout = 20_000;

/** Imports url from the library and waits for its document page; returns the document's path. */
export async function importPage(page: Page, url: string): Promise<string> {
  await startImport(page, url);
  await expect(page).toHaveURL(/\/doc\/[0-9a-f-]{36}$/, { timeout: importTimeout });
  return new URL(page.url()).pathname;
}

/** Goes back to the library through the sidebar, as a reader would. */
export async function openLibrary(page: Page) {
  await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link", { name: "Library" })
    .click();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();
}

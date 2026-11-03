import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { apiError, json, stubApi } from "../test/api";
import { send, subscriptions } from "../test/events";
import { renderApp } from "../test/render";

vi.mock("../api/events", () => import("../test/events"));

beforeEach(() => {
  subscriptions.length = 0;
});

function summary(n: number) {
  return {
    id: `doc-${n}`,
    title: `Doc ${n}`,
    source_url: `https://docs.example.com/${n}`,
    version: 1,
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
  };
}

/** Answers the library's pages from pages, and the sidebar with recent. */
function stubDocuments(pages: Record<string, ReturnType<typeof summary>[]>, recent = [summary(1)]) {
  const cursors = Object.keys(pages);
  return stubApi({
    "GET /documents": (request) => {
      const query = new URL(request.url).searchParams;
      if (query.get("limit") === "8") return json({ documents: recent });
      const at = cursors.indexOf(query.get("cursor") ?? "");
      const next = cursors[at + 1];
      return json({
        documents: pages[cursors[at] ?? ""] ?? [],
        ...(next ? { next_cursor: next } : {}),
      });
    },
  });
}

/** Renders the app at the library, once its main region is there. */
async function openLibrary() {
  const app = renderApp("/");
  return { ...app, library: within(await screen.findByRole("main")) };
}

function titles(links: HTMLElement[]) {
  return links.map((link) => link.textContent);
}

test("lists documents a page at a time", async () => {
  const requests = stubDocuments({ "": [summary(1), summary(2)], c1: [summary(3)] });
  const { library } = await openLibrary();

  expect(await library.findByRole("link", { name: "Doc 1" })).toBeDefined();
  expect(titles(library.getAllByRole("link"))).toEqual(["Doc 1", "Doc 2"]);
  const row = library.getByRole("link", { name: "Doc 2" }).closest("li");
  expect(row?.textContent).toContain("docs.example.com");
  expect(row?.querySelector("time")?.getAttribute("dateTime")).toBe("2026-11-03T09:00:00Z");

  fireEvent.click(library.getByRole("button", { name: "Load more" }));

  expect(await library.findByRole("link", { name: "Doc 3" })).toBeDefined();
  expect(titles(library.getAllByRole("link"))).toEqual(["Doc 1", "Doc 2", "Doc 3"]);
  expect(library.queryByRole("button", { name: "Load more" })).toBeNull();
  const pageQueries = requests
    .map((r) => new URL(r.url).searchParams)
    .filter((q) => q.get("limit") === "20")
    .map((q) => q.get("cursor"));
  expect(pageQueries).toEqual([null, "c1"]);
});

test("shows still placeholder rows while loading", async () => {
  stubApi({ "GET /documents": () => new Promise<Response>(() => {}) });
  const { library } = await openLibrary();

  expect((await library.findByRole("status")).textContent).toBe("Loading your library");
  const placeholder = library.getByRole("status").nextElementSibling;
  expect(placeholder?.getAttribute("aria-hidden")).toBe("true");
  expect(placeholder?.querySelectorAll("li")).toHaveLength(3);
  expect(placeholder?.outerHTML).not.toMatch(/animate-/);
});

test("offers a retry when the library fails to load", async () => {
  let fail = true;
  stubApi({
    "GET /documents": () =>
      fail ? apiError(500, "internal_error") : json({ documents: [summary(1)] }),
  });
  const { library } = await openLibrary();

  expect(await library.findByText("Couldn't load your library.")).toBeDefined();
  fail = false;
  fireEvent.click(library.getByRole("button", { name: "Retry" }));

  expect(await library.findByRole("link", { name: "Doc 1" })).toBeDefined();
});

test("keeps its empty state", async () => {
  stubDocuments({ "": [] }, []);
  const { library } = await openLibrary();

  expect(
    await library.findByText(
      "No pages yet. Import a documentation page or a printed PDF to start.",
    ),
  ).toBeDefined();
});

describe("imports from this session", () => {
  const imp = {
    id: "imp-1",
    requested_url: "https://docs.example.com/new",
    canonical_url: "https://docs.example.com/new",
    status: "queued" as const,
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
  };

  test("show as pending rows until they finish", async () => {
    stubDocuments({ "": [summary(1)] });
    const { tracker, library } = await openLibrary();
    await library.findByRole("link", { name: "Doc 1" });

    tracker.track(imp);
    const row = await library.findByText("https://docs.example.com/new");
    expect(row.closest("li")?.textContent).toContain("Queued");
    send({ status: "fetching" });
    expect(row.closest("li")?.textContent).toContain("Fetching the page");

    send({ status: "done", document_id: "doc-9" });

    expect(library.queryByText("https://docs.example.com/new")).toBeNull();
  });

  test("say why they failed until dismissed", async () => {
    stubDocuments({ "": [] }, []);
    const { tracker, library } = await openLibrary();
    tracker.track(imp);
    const row = (await library.findByText("https://docs.example.com/new")).closest("li");

    send({ status: "failed", error_code: "disallowed_by_robots" });

    expect(row?.textContent).toContain("This site doesn't allow RoNote to fetch its pages.");
    fireEvent.click(library.getByRole("button", { name: "Dismiss" }));
    expect(library.queryByText("https://docs.example.com/new")).toBeNull();
    expect(
      library.getByText("No pages yet. Import a documentation page or a printed PDF to start."),
    ).toBeDefined();
  });
});

test("the sidebar lists the eight newest documents", async () => {
  const recent = [1, 2, 3, 4, 5, 6, 7, 8].map(summary);
  stubDocuments({ "": recent }, recent);
  await openLibrary();

  const nav = within(screen.getByRole("navigation", { name: "Main" }));
  const section = within(await nav.findByRole("region", { name: "Recent" }));
  const links = section.getAllByRole("link");

  expect(links.map((l) => l.textContent)).toEqual(recent.map((d) => d.title));
  expect(links[0]?.getAttribute("href")).toBe("/doc/doc-1");
});

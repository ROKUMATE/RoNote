import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";
import type { SearchResult } from "../api/schema";
import { json, stubApi } from "../test/api";
import { renderApp } from "../test/render";
import { degradedMessage } from "./SearchPalette";

const goroutines: SearchResult = {
  document_id: "doc-go",
  document_title: "Effective Go",
  block_id: "b-goroutines",
  heading_path: ["Concurrency", "Goroutines"],
  kind: "prose",
  snippet: [
    { text: "They're called ", mark: false },
    { text: "goroutines", mark: true },
  ],
};
const channels: SearchResult = {
  ...goroutines,
  block_id: "b-channels",
  heading_path: ["Concurrency", "Channels"],
};
const threads: SearchResult = {
  ...goroutines,
  document_id: "doc-py",
  document_title: "Python threads",
  block_id: "b-threads",
  heading_path: [],
};

function stubSearch(results: SearchResult[], degraded = false) {
  return stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /search": () => json({ results, degraded }),
    "GET /documents/doc-go": () =>
      json({ error: { code: "not_found", message: "", request_id: "t" } }, 404),
    "GET /documents/doc-1": () =>
      json({
        id: "doc-1",
        title: "Guide",
        source_url: "https://docs.example.com/guide",
        version: 1,
        created_at: "2026-11-03T09:00:00Z",
        updated_at: "2026-11-03T09:00:00Z",
        content: {
          doc_version: 1,
          blocks: [
            {
              id: "p",
              kind: "paragraph",
              content: [{ text: "Hello" }],
              origin: "source",
              edited: false,
              source_block_id: "p",
            },
          ],
        },
        warnings: [],
      }),
  });
}

async function openPalette() {
  // The shortcut works once the app's frame is up.
  await screen.findByRole("button", { name: /^Search/ });
  fireEvent.keyDown(window, { key: "k", ctrlKey: true });
  return screen.findByRole("dialog", { name: "Search your library" });
}

test("Mod+K opens the palette; results group by document; Enter opens the active one at its block", async () => {
  const requests = stubSearch([goroutines, channels, threads]);
  const app = renderApp("/");
  const palette = await openPalette();
  const box = within(palette).getByRole("combobox", { name: "Search your library" });

  fireEvent.change(box, { target: { value: "  run things at once " } });

  const groups = await within(palette).findAllByRole("group");
  expect(groups.map((g) => g.getAttribute("aria-label"))).toEqual([
    "Effective Go",
    "Python threads",
  ]);
  expect(within(groups[0] as HTMLElement).getAllByRole("option")).toHaveLength(2);
  expect(within(palette).getAllByText("goroutines")[0]?.tagName).toBe("MARK");
  const search = requests.find((r) => new URL(r.url).pathname.endsWith("/search"));
  expect(new URL(search?.url ?? "").searchParams.get("q")).toBe("run things at once");

  fireEvent.keyDown(box, { key: "ArrowDown" });
  expect(box.getAttribute("aria-activedescendant")).toBe(
    within(palette).getAllByRole("option")[1]?.id,
  );
  fireEvent.keyDown(box, { key: "Enter" });

  await waitFor(() => expect(app.router.state.location.pathname).toBe("/doc/doc-go"));
  expect(app.router.state.location.search).toEqual({ block: "b-channels" });
  expect(screen.queryByRole("dialog", { name: "Search your library" })).toBeNull();
});

test("the Search button opens it too", async () => {
  stubSearch([]);
  renderApp("/");

  fireEvent.click(await screen.findByRole("button", { name: /^Search/ }));

  expect(await screen.findByRole("dialog", { name: "Search your library" })).toBeDefined();
});

test("word matches alone say so, and no matches say so", async () => {
  stubSearch([], true);
  renderApp("/");
  const palette = await openPalette();

  fireEvent.change(within(palette).getByRole("combobox"), { target: { value: "goroutines" } });

  expect((await within(palette).findByText(degradedMessage)).getAttribute("role")).toBe("status");
  expect(within(palette).getByText("No matches.")).toBeDefined();
});

test("from a document, This document searches it alone", async () => {
  const requests = stubSearch([goroutines]);
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  await screen.findByRole("article");
  const palette = await openPalette();

  fireEvent.click(within(palette).getByRole("checkbox", { name: "This document" }));
  fireEvent.change(within(palette).getByRole("combobox"), { target: { value: "hello" } });

  await within(palette).findAllByRole("option");
  const search = requests.filter((r) => new URL(r.url).pathname.endsWith("/search")).at(-1);
  expect(new URL(search?.url ?? "").searchParams.get("document_id")).toBe("doc-1");
});

test("in the library there is no document to keep to", async () => {
  stubSearch([]);
  renderApp("/");
  const palette = await openPalette();

  expect(within(palette).queryByRole("checkbox", { name: "This document" })).toBeNull();
});

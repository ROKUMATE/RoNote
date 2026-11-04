import { screen, waitFor, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test, vi } from "vitest";
import { json, stubApi } from "../test/api";
import { renderApp } from "../test/render";

const heading = (id: string, text: string, anchor: string) => ({
  id,
  kind: "heading",
  level: 2,
  anchor,
  content: [{ text }],
  origin: "source",
  edited: false,
  source_block_id: id,
});

const doc = {
  id: "doc-b",
  title: "Guide B",
  source_url: "https://docs.example.com/b",
  version: 1,
  created_at: "2026-11-04T09:00:00Z",
  updated_at: "2026-11-04T09:00:00Z",
  warnings: [],
  content: {
    doc_version: 1,
    blocks: [heading("h1", "Background", "background"), heading("h2", "Usage", "usage")],
  },
};

test("arriving with a #fragment scrolls to that heading and puts the caret there", async () => {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-b": () => json(doc),
  });
  const app = renderApp("/");

  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-b" }, hash: "usage" });

  const main = within(await screen.findByRole("main"));
  const usage = await main.findByRole("heading", { name: "Usage" });
  await waitFor(() => expect(scrolled.mock.contexts).toEqual([usage]));
  const editor = (document.querySelector(".ProseMirror") as HTMLElement & { editor: Editor })
    .editor;
  expect(editor.state.selection.$from.parent.textContent).toBe("Usage");
});

test("a #fragment naming nothing in the document leaves the page where it opens", async () => {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-b": () => json(doc),
  });
  const app = renderApp("/");

  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-b" }, hash: "nowhere" });

  await within(await screen.findByRole("main")).findByRole("heading", { name: "Usage" });
  await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
  await new Promise((done) => requestAnimationFrame(done));
  expect(scrolled).not.toHaveBeenCalled();
});

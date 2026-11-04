import { act, screen, waitFor, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test } from "vitest";
import type { DocContent } from "../api/schema";
import { apiError, json, stubApi } from "../test/api";
import { renderApp } from "../test/render";

const doc = {
  id: "doc-1",
  title: "Guide",
  source_url: "https://docs.example.com/guide",
  version: 3,
  created_at: "2026-11-03T09:00:00Z",
  updated_at: "2026-11-03T09:00:00Z",
  warnings: [],
  content: {
    doc_version: 1,
    blocks: [
      {
        id: "p1",
        kind: "paragraph",
        content: [{ text: "Install it." }],
        origin: "source",
        edited: false,
        source_block_id: "p1",
      },
      {
        id: "c1",
        kind: "code",
        text: "go install .\n",
        origin: "source",
        edited: false,
        source_block_id: "c1",
      },
    ],
  },
};

async function openAndEdit(save: (request: Request) => Response | Promise<Response>) {
  const saves: { content: DocContent; version: number }[] = [];
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => json(doc),
    "PATCH /documents/doc-1": async (request) => {
      saves.push(await request.clone().json());
      return save(request);
    },
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  const main = within(await screen.findByRole("main"));
  await main.findByText("Install it.");
  const editor = (document.querySelector(".ProseMirror") as HTMLElement & { editor: Editor })
    .editor;
  act(() => {
    editor.chain().setTextSelection(11).insertContent(" now").run();
  });
  return { main, saves, editor };
}

test("an edit is saved a moment later, from the loaded version", async () => {
  const { main, saves } = await openAndEdit(() => json({ version: 4 }));

  await waitFor(() => expect(main.getByRole("status").textContent).toBe("Saved"), {
    timeout: 3000,
  });

  expect(saves).toHaveLength(1);
  const [saved] = saves;
  expect(saved?.version).toBe(3);
  expect(saved?.content.blocks[0]).toMatchObject({
    id: "p1",
    edited: true,
    content: [{ text: "Install it now." }],
  });
  // The code block was not touched: it is saved exactly as loaded.
  expect(saved?.content.blocks[1]).toEqual(doc.content.blocks[1]);
});

test("a conflict says so, offers Reload, and saves nothing more", async () => {
  const { main, saves, editor } = await openAndEdit(() =>
    json(
      { error: { code: "version_conflict", message: "", request_id: "" }, current_version: 5 },
      409,
    ),
  );

  const alert = await main.findByRole("alert", {}, { timeout: 3000 });
  expect(alert.textContent).toContain("This document changed in another tab.");
  expect(within(alert).getByRole("button", { name: "Reload" })).toBeDefined();

  act(() => {
    editor.chain().insertContent(" again").run();
  });
  await new Promise((resolve) => setTimeout(resolve, 1300));
  expect(saves).toHaveLength(1);
});

test("a save the server refuses says why", async () => {
  const { main } = await openAndEdit(() => apiError(422, "source_block_modified"));

  const alert = await main.findByRole("alert", {}, { timeout: 3000 });
  expect(alert.textContent).toContain("Code, tables and tabs from the page can't be edited.");
});

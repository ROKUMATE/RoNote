import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test } from "vitest";
import type { Annotation, TextSelector } from "../api/schema";
import { selectorOf } from "../document/anchoring";
import { json, stubApi } from "../test/api";
import { renderApp } from "../test/render";

const text = "Install the tool before you start.";
const source = { origin: "source", edited: false } as const;

const doc = {
  id: "doc-1",
  title: "Guide",
  source_url: "https://docs.example.com/guide",
  version: 3,
  created_at: "2026-11-05T09:00:00Z",
  updated_at: "2026-11-05T09:00:00Z",
  warnings: [],
  content: {
    doc_version: 1,
    blocks: [
      { id: "p1", kind: "paragraph", content: [{ text }], ...source, source_block_id: "p1" },
    ],
  },
};

function annotation(id: string, selector: TextSelector, note: string | null = null): Annotation {
  return {
    id,
    document_id: "doc-1",
    block_id: "p1",
    kind: "highlight",
    selector,
    note,
    version: 1,
    created_at: "2026-11-05T09:00:00Z",
    updated_at: "2026-11-05T09:00:00Z",
  };
}

const theTool = selectorOf(text, 8, 16);

async function openDoc(existing: Annotation[] = []) {
  const posted: unknown[] = [];
  const patched: unknown[] = [];
  const deleted: string[] = [];
  const reanchored: unknown[] = [];
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => json(doc),
    "GET /documents/doc-1/annotations": () => json({ annotations: existing }),
    "POST /documents/doc-1/annotations": async (request) => {
      const body = await request.clone().json();
      posted.push(body);
      return json(annotation(`new-${posted.length}`, body.selector, body.note ?? null), 201);
    },
    "PATCH /annotations/a1": async (request) => {
      const body = await request.clone().json();
      patched.push(body);
      return json({ ...annotation("a1", theTool, body.note || null), version: 2 });
    },
    "PATCH /annotations/new-1": async (request) => {
      const body = await request.clone().json();
      patched.push(body);
      return json({ ...annotation("new-1", theTool, body.note || null), version: 2 });
    },
    "DELETE /annotations/a1": () => {
      deleted.push("a1");
      return new Response(null, { status: 204 });
    },
    "PATCH /documents/doc-1": () => json({ version: 4 }),
    "POST /documents/doc-1/annotations/reanchor": async (request) => {
      const body = await request.clone().json();
      reanchored.push(body);
      return json({
        annotations: body.annotations.map((m: { id: string; selector: TextSelector }) => ({
          ...annotation(m.id, m.selector),
          version: 2,
        })),
      });
    },
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  const main = within(await screen.findByRole("main"));
  await main.findByText(/Install the/);
  const editor = (document.querySelector(".ProseMirror") as HTMLElement & { editor: Editor })
    .editor;
  return { main, editor, posted, patched, deleted, reanchored };
}

function select(editor: Editor, from: number, to: number) {
  act(() => {
    editor
      .chain()
      .focus()
      .setTextSelection({ from: from + 1, to: to + 1 })
      .run();
  });
}

test("selecting text offers Highlight, which saves and draws it", async () => {
  const { main, editor, posted } = await openDoc();
  select(editor, 8, 16);

  const toolbar = await main.findByRole("toolbar", { name: "Highlight" });
  fireEvent.click(within(toolbar).getByRole("button", { name: "Highlight" }));

  await waitFor(() =>
    expect(posted).toEqual([{ block_id: "p1", kind: "highlight", selector: theTool }]),
  );
  await waitFor(() => expect(document.querySelector("mark.hl")?.textContent).toBe("the tool"));
  expect(main.queryByRole("toolbar")).toBeNull();
});

test("Highlight + note opens the note, and Enter saves it", async () => {
  const { main, editor, patched } = await openDoc();
  select(editor, 8, 16);

  fireEvent.click(
    within(await main.findByRole("toolbar")).getByRole("button", { name: "Highlight + note" }),
  );

  const note = await main.findByRole("textbox", { name: "Note" });
  await waitFor(() => expect(document.activeElement).toBe(note));
  fireEvent.change(note, { target: { value: "Needs Go 1.26" } });
  fireEvent.keyDown(note, { key: "Enter" });
  await waitFor(() => expect(patched).toEqual([{ version: 1, note: "Needs Go 1.26" }]));
  await waitFor(() => expect(main.queryByRole("dialog")).toBeNull());
});

test("Ctrl+Shift+H highlights the selection without the toolbar", async () => {
  const { editor, posted } = await openDoc();
  select(editor, 0, 7);

  // As browsers send it: Shift makes the key "H", the key code stays H's.
  fireEvent.keyDown(editor.view.dom, { key: "H", keyCode: 72, ctrlKey: true, shiftKey: true });

  await waitFor(() =>
    expect(posted).toEqual([
      { block_id: "p1", kind: "highlight", selector: selectorOf(text, 0, 7) },
    ]),
  );
});

test("a click on a highlight opens it; Remove can be undone", async () => {
  const { main, deleted, posted } = await openDoc([annotation("a1", theTool, "a note")]);
  const mark = await waitFor(() => {
    const found = document.querySelector("mark.hl");
    if (!found) throw new Error("no highlight yet");
    return found;
  });

  fireEvent.click(mark);
  const dialog = await main.findByRole("dialog", { name: "Highlight" });
  expect(within(dialog).getByRole("textbox", { name: "Note" })).toHaveProperty("value", "a note");
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove highlight" }));

  const status = await screen.findByText(/Highlight removed/);
  await waitFor(() => expect(document.querySelector("mark.hl")).toBeNull());
  expect(deleted).toEqual(["a1"]);
  fireEvent.click(within(status).getByRole("button", { name: "Undo" }));
  await waitFor(() =>
    expect(posted).toEqual([
      { block_id: "p1", kind: "highlight", selector: theTool, note: "a note" },
    ]),
  );
  await waitFor(() => expect(document.querySelector("mark.hl")?.textContent).toBe("the tool"));
});

test("after an edit is saved, moved highlights are saved where their words are now", async () => {
  const { editor, reanchored } = await openDoc([annotation("a1", theTool)]);
  await waitFor(() => expect(document.querySelector("mark.hl")).not.toBeNull());

  act(() => {
    editor.chain().setTextSelection(1).insertContent("First, ").run();
  });

  await waitFor(() => expect(reanchored).toHaveLength(1), { timeout: 4000 });
  expect(reanchored[0]).toEqual({
    annotations: [{ id: "a1", version: 1, selector: selectorOf(`First, ${text}`, 15, 23) }],
  });
  expect(document.querySelector("mark.hl")?.textContent).toBe("the tool");
});

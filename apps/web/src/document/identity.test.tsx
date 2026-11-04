import { act, fireEvent, render, screen } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test, vi } from "vitest";
import type { DocBlock, DocContent } from "../api/schema";
import { fromEditor } from "./mapping";
import { Reader } from "./Reader";

const p1: DocBlock = {
  id: "p1",
  kind: "paragraph",
  content: [{ text: "Install it." }],
  origin: "source",
  edited: false,
  source_block_id: "p1",
};
const list: DocBlock = {
  id: "l1",
  kind: "list",
  items: [{ children: [{ id: "lp1", kind: "paragraph", content: [{ text: "One" }] }] }],
  origin: "source",
  edited: false,
  source_block_id: "l1",
};
const note: DocBlock = {
  id: "0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10",
  kind: "paragraph",
  content: [{ text: "Mine." }],
  origin: "user",
};

async function openReader(onRevert?: (blockId: string) => Promise<DocBlock>) {
  const content: DocContent = { doc_version: 1, blocks: [p1, list, note] };
  let editor: Editor | undefined;
  render(
    <Reader content={content} onEditor={(e) => (editor = e)} {...(onRevert ? { onRevert } : {})} />,
  );
  await screen.findByText("Install it.");
  if (!editor) throw new Error("no editor");
  return editor;
}

function topLevel(editor: Editor) {
  const nodes: Record<string, unknown>[] = [];
  editor.state.doc.forEach((node) => {
    nodes.push(node.attrs);
  });
  return nodes;
}

const uuidv7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

test("changing a source block marks it edited, with a label and Revert to source", async () => {
  const editor = await openReader();
  expect(screen.queryByText("edited")).toBeNull();

  act(() => {
    editor.chain().setTextSelection(11).insertContent(" now").run();
  });

  expect(topLevel(editor)[0]).toMatchObject({ blockId: "p1", origin: "source", edited: true });
  expect(document.querySelector('[data-block-id="p1"]')?.getAttribute("data-edited")).toBe("true");
  expect(screen.getByText("edited")).toBeDefined();
  expect(screen.getByRole("button", { name: "Revert to source" })).toBeDefined();
  const saved = fromEditor(editor.getJSON(), new Map([["p1", p1]])).blocks[0];
  expect(saved).toMatchObject({ edited: true, content: [{ text: "Install it now." }] });
});

test("undoing the change makes it unedited again", async () => {
  const editor = await openReader();
  act(() => {
    editor.chain().setTextSelection(11).insertContent(" now").run();
  });

  act(() => {
    editor.commands.undo();
  });

  expect(topLevel(editor)[0]).toMatchObject({ edited: false });
  expect(screen.queryByText("edited")).toBeNull();
});

test("a user block is never marked edited", async () => {
  const editor = await openReader();
  const end = editor.state.doc.content.size - 1;

  act(() => {
    editor.chain().setTextSelection(end).insertContent(" Still mine.").run();
  });

  expect(topLevel(editor)[2]).toMatchObject({ origin: "user", edited: null });
  expect(screen.queryByText("edited")).toBeNull();
});

test("splitting a source block keeps the page's half as source and makes the rest a user block", async () => {
  const editor = await openReader();

  act(() => {
    editor.chain().setTextSelection(8).splitBlock().run();
  });

  const [first, second] = topLevel(editor);
  expect(first).toMatchObject({ blockId: "p1", origin: "source", edited: true });
  expect(second).toMatchObject({ origin: "user", edited: null, sourceBlockId: null });
  expect(second?.blockId).toMatch(uuidv7);
});

test("splitting inside a list gives the new item's blocks ids of their own", async () => {
  const editor = await openReader();
  // After the paragraph (13 positions), into the list, its item and the
  // item's paragraph, at the end of "One".
  const inItem = 13 + 3 + "One".length;

  act(() => {
    editor.chain().setTextSelection(inItem).splitListItem("listItem").run();
  });

  const ids: string[] = [];
  editor.state.doc.descendants((node) => {
    if (node.attrs.blockId) ids.push(node.attrs.blockId);
  });
  expect(new Set(ids).size).toBe(ids.length);
  expect(topLevel(editor)[1]).toMatchObject({ blockId: "l1", edited: true });
});

test("Revert to source asks first, then puts the stored block back unedited", async () => {
  const onRevert = vi.fn(async () => p1);
  const editor = await openReader(onRevert);
  act(() => {
    editor.chain().setTextSelection(11).insertContent(" now").run();
  });

  fireEvent.click(screen.getByRole("button", { name: "Revert to source" }));
  const dialog = screen.getByRole("dialog", { name: "Revert to source?" });
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onRevert).not.toHaveBeenCalled();
  expect(dialog.isConnected).toBe(false);

  fireEvent.click(screen.getByRole("button", { name: "Revert to source" }));
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Revert" }));
  });

  expect(onRevert).toHaveBeenCalledWith("p1");
  expect(topLevel(editor)[0]).toMatchObject({ blockId: "p1", origin: "source", edited: false });
  expect(screen.queryByText("edited")).toBeNull();
  expect(fromEditor(editor.getJSON(), new Map([["p1", p1]])).blocks[0]).toBe(p1);
});

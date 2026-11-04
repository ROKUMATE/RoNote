import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test } from "vitest";
import type { DocContent } from "../api/schema";
import { specEnum } from "../test/spec";
import { fromEditor } from "./mapping";
import { Reader } from "./Reader";

const content: DocContent = {
  doc_version: 1,
  blocks: [
    {
      id: "p1",
      kind: "paragraph",
      content: [{ text: "Source text." }],
      origin: "source",
      edited: false,
      source_block_id: "p1",
    },
  ],
};

async function openReader() {
  let editor: Editor | undefined;
  render(<Reader content={content} onEditor={(e) => (editor = e)} />);
  await screen.findByText("Source text.");
  if (!editor) throw new Error("no editor");
  return editor;
}

const uuidv7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

test("the menu offers exactly the six user block kinds, in the spec's order", async () => {
  const editor = await openReader();
  act(() => {
    editor.commands.setTextSelection(3);
  });

  fireEvent.click(screen.getByRole("button", { name: "Add a block below" }));

  const items = within(screen.getByRole("menu", { name: "Add a block" })).getAllByRole("menuitem");
  expect(items.map((i) => i.dataset.kind)).toEqual(specEnum("UserBlockKind"));
  expect(items.map((i) => i.textContent)).toEqual([
    "Text",
    "Heading",
    "List",
    "Quote",
    "Callout",
    "Divider",
  ]);
  expect(document.activeElement).toBe(items[0]);
});

test('"+" adds a user block below the block at the cursor', async () => {
  const editor = await openReader();
  act(() => {
    editor.commands.setTextSelection(3);
  });
  fireEvent.click(screen.getByRole("button", { name: "Add a block below" }));

  fireEvent.click(screen.getByRole("menuitem", { name: "Quote" }));
  act(() => {
    editor.commands.insertContent("My note");
  });

  const blocks = fromEditor(editor.getJSON(), new Map()).blocks;
  expect(blocks.map((b) => [b.kind, b.origin])).toEqual([
    ["paragraph", "source"],
    ["quote", "user"],
  ]);
  expect(blocks[1]?.id).toMatch(uuidv7);
  expect(blocks[1]?.children?.[0]?.content).toEqual([{ text: "My note" }]);
  expect(screen.queryByRole("menu")).toBeNull();
});

test('"/" in an empty paragraph opens the menu in its place; arrows and Escape work', async () => {
  const editor = await openReader();
  act(() => {
    editor.commands.setTextSelection(1 + "Source text.".length);
    editor.commands.splitBlock();
  });
  const emptyAt = editor.state.selection.$from.before(1);

  fireEvent.keyDown(editor.view.dom, { key: "/" });

  const menu = screen.getByRole("menu", { name: "Add a block" });
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  expect(document.activeElement?.textContent).toBe("Heading");
  fireEvent.keyDown(menu, { key: "ArrowUp" });
  fireEvent.keyDown(menu, { key: "ArrowUp" });
  expect(document.activeElement?.textContent).toBe("Divider");
  fireEvent.keyDown(menu, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();

  fireEvent.keyDown(editor.view.dom, { key: "/" });
  fireEvent.click(screen.getByRole("menuitem", { name: "Heading" }));

  const replaced = editor.state.doc.nodeAt(emptyAt);
  expect(replaced?.type.name).toBe("heading");
  expect(replaced?.attrs.origin).toBe("user");
  expect(replaced?.attrs.blockId).toMatch(uuidv7);
  expect(editor.state.doc.childCount).toBe(2);
});

test('"/" in a paragraph with text does not open the menu', async () => {
  const editor = await openReader();
  act(() => {
    editor.commands.setTextSelection(3);
  });

  fireEvent.keyDown(editor.view.dom, { key: "/" });

  expect(screen.queryByRole("menu")).toBeNull();
});

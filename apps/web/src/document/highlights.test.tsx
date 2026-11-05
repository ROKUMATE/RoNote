import { act, render, screen, waitFor } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test } from "vitest";
import type { DocBlock, IrBlock } from "../api/schema";
import { selectorOf } from "./anchoring";
import { type Highlight, placements, selectionSegments, setHighlights } from "./highlights";
import { Reader } from "./Reader";

const source = (b: IrBlock): DocBlock => ({
  ...b,
  origin: "source",
  edited: false,
  source_block_id: b.id,
});

const first = "Install the tool before you start.";
const second = "Then run the tool on a module.";
const code = "go install ./...\n";

const blocks = [
  source({ id: "p1", kind: "paragraph", content: [{ text: first }] }),
  source({
    id: "l1",
    kind: "list",
    items: [{ children: [{ id: "lp", kind: "paragraph", content: [{ text: second }] }] }],
  }),
  source({ id: "c1", kind: "code", text: code }),
];

function highlight(id: string, blockId: string, text: string, quote: string): Highlight {
  const start = text.indexOf(quote);
  return { id, block_id: blockId, selector: selectorOf(text, start, start + quote.length) };
}

async function openReader(highlights: Highlight[]) {
  let editor: Editor | undefined;
  render(<Reader content={{ doc_version: 1, blocks }} onEditor={(e) => (editor = e)} />);
  await screen.findByText(/Install the/);
  if (!editor) throw new Error("no editor");
  const e = editor;
  act(() => setHighlights(e.view, highlights));
  return e;
}

const marks = () =>
  [...document.querySelectorAll("mark.hl")].map((m) => [
    m.getAttribute("data-annotation"),
    m.textContent,
  ]);

test("highlights are drawn as marks over their words, nested blocks included", async () => {
  const editor = await openReader([
    highlight("a1", "p1", first, "the tool"),
    highlight("a2", "lp", second, "run the tool"),
    highlight("a3", "c1", code, "install"),
  ]);

  // Code is drawn by its own view, from the same placements.
  await waitFor(() =>
    expect(marks()).toEqual([
      ["a1", "the tool"],
      ["a2", "run the tool"],
      ["a3", "install"],
    ]),
  );
  expect(placements(editor.state).placed.get("a3")).toEqual({ start: 3, end: 10 });
  expect(document.querySelector("pre")?.textContent).toBe(code);
});

test("a highlight follows its words as text is typed before it, and nothing is an edit", async () => {
  const editor = await openReader([highlight("a1", "p1", first, "the tool")]);
  const before = JSON.stringify(editor.getJSON());

  act(() => {
    editor.chain().setTextSelection(1).insertContent("First, ").run();
  });

  expect(marks()).toEqual([["a1", "the tool"]]);
  expect(placements(editor.state).placed.get("a1")).toEqual({ start: 15, end: 23 });
  act(() => {
    editor.commands.undo();
  });
  expect(JSON.stringify(editor.getJSON())).toBe(before);
});

test("deleting the highlighted words leaves the highlight unplaced", async () => {
  const editor = await openReader([highlight("a1", "p1", first, "the tool")]);

  act(() => {
    editor.chain().setTextSelection({ from: 9, to: 18 }).deleteSelection().run();
  });

  expect(marks()).toEqual([]);
  expect(placements(editor.state).placed.has("a1")).toBe(false);
  expect(placements(editor.state).highlights.map((h) => h.id)).toEqual(["a1"]);
});

test("a selection across blocks is cut into one segment per block", async () => {
  const editor = await openReader([]);
  // From "before" in the first paragraph to "run" in the list's paragraph.
  const from = 1 + first.indexOf("before");
  let to = 0;
  editor.state.doc.descendants((node, pos) => {
    if (node.attrs.blockId === "lp") to = pos + 1 + second.indexOf("run") + 3;
  });

  act(() => {
    editor.commands.setTextSelection({ from, to });
  });

  expect(selectionSegments(editor.state)).toEqual([
    { blockId: "p1", selector: selectorOf(first, first.indexOf("before"), first.length) },
    { blockId: "lp", selector: selectorOf(second, 0, second.indexOf("run") + 3) },
  ]);
});

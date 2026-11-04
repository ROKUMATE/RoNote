import { Editor } from "@tiptap/core";
import { describe, expect, test } from "vitest";
import type { DocBlock, DocContent, IrBlock } from "../api/schema";
import { fromEditor, toEditor } from "./mapping";
import { extensions } from "./schema";
import { uuidv7 } from "./uuid";

const goldens = import.meta.glob<{ blocks: IrBlock[] }>(
  "../../../../testdata/golden/*/expected.json",
  { eager: true, import: "default" },
);

function asSource(blocks: IrBlock[]): DocContent {
  return {
    doc_version: 1,
    blocks: blocks.map((b) => ({ ...b, origin: "source", edited: false, source_block_id: b.id })),
  };
}

/** content loaded into a real editor and read back out of it. */
function throughEditor(content: DocContent, originals = new Map<string, DocBlock>()) {
  const editor = new Editor({ extensions, content: toEditor(content) });
  try {
    return fromEditor(editor.getJSON(), originals);
  } finally {
    editor.destroy();
  }
}

describe("every golden fixture", () => {
  test("has fixtures", () => {
    expect(Object.keys(goldens).length).toBeGreaterThan(40);
  });

  for (const [path, golden] of Object.entries(goldens)) {
    const name = path.split("/").at(-2);
    test(`${name} round-trips through the editor unchanged`, () => {
      const content = asSource(golden.blocks);

      // No originals: every block is read back from the editor.
      expect(throughEditor(content)).toEqual(content);
    });
  }
});

describe("blocks read from the editor", () => {
  const paragraph = (id: string, text: string): IrBlock => ({
    id,
    kind: "paragraph",
    content: [{ text }],
  });

  test("an unedited source block is kept exactly as loaded", () => {
    // Marks out of the IR's order: the editor would reorder them.
    const original: DocBlock = {
      id: "p1",
      kind: "paragraph",
      content: [{ text: "x", marks: ["italic", "bold"] }],
      origin: "source",
      edited: false,
      source_block_id: "p1",
    };
    const content: DocContent = { doc_version: 1, blocks: [original] };

    expect(throughEditor(content, new Map([["p1", original]])).blocks[0]).toBe(original);
    expect(throughEditor(content).blocks[0]).toMatchObject({
      content: [{ text: "x", marks: ["bold", "italic"] }],
    });
  });

  test("an edited source block and a user block are read from the editor", () => {
    const note = uuidv7();
    const content: DocContent = {
      doc_version: 1,
      blocks: [
        { ...paragraph("p1", "Changed"), origin: "source", edited: true, source_block_id: "p1" },
        { ...paragraph(note, "Mine"), origin: "user" },
      ],
    };
    const originals = new Map<string, DocBlock>([
      [
        "p1",
        { ...paragraph("p1", "Original"), origin: "source", edited: false, source_block_id: "p1" },
      ],
    ]);

    expect(throughEditor(content, originals)).toEqual(content);
  });

  test("blocks emptied by editing are dropped", () => {
    const content: DocContent = {
      doc_version: 1,
      blocks: [{ ...paragraph(uuidv7(), "x"), origin: "user" }],
    };
    const editor = new Editor({ extensions, content: toEditor(content) });
    editor.commands.setContent({
      type: "doc",
      content: [
        { type: "paragraph", attrs: { origin: "user" } },
        {
          type: "list",
          attrs: { origin: "user" },
          content: [{ type: "listItem", content: [{ type: "paragraph" }] }],
        },
      ],
    });

    expect(fromEditor(editor.getJSON(), new Map()).blocks).toEqual([]);
    editor.destroy();
  });

  test("a user block without an id gets a UUIDv7", () => {
    const json = {
      type: "doc",
      content: [
        { type: "paragraph", attrs: { origin: "user" }, content: [{ type: "text", text: "x" }] },
      ],
    };

    const [block] = fromEditor(json, new Map()).blocks;

    expect(block?.id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });
});

test("UUIDv7s carry their time and sort by it", () => {
  const early = uuidv7(Date.UTC(2026, 10, 4));
  const late = uuidv7(Date.UTC(2026, 10, 5));

  expect(early < late).toBe(true);
  expect(Number.parseInt(early.replaceAll("-", "").slice(0, 12), 16)).toBe(Date.UTC(2026, 10, 4));
});

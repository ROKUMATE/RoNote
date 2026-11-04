import { type Editor, Extension } from "@tiptap/core";
import type { UserBlockKind } from "../api/schema";
import { newUserBlock } from "./userBlocks";

/** Where a new block goes: in place of an empty paragraph, or after a block. */
export type InsertAt = { replace: number } | { after: number };

/**
 * Opens the block menu when "/" is typed into an empty top-level paragraph,
 * instead of typing the slash.
 */
export const SlashMenu = Extension.create<{ open: (at: InsertAt) => void }>({
  name: "slashMenu",
  addOptions() {
    return { open: () => {} };
  },
  addKeyboardShortcuts() {
    return {
      "/": ({ editor }) => {
        const { $from, empty } = editor.state.selection;
        const block = $from.depth === 1 ? $from.parent : undefined;
        if (!empty || block?.type.name !== "paragraph" || block.content.size > 0) return false;
        this.options.open({ replace: $from.before(1) });
        return true;
      },
    };
  },
});

/** The start of the top-level block holding pos, or undefined. */
export function topLevelBlockAt(editor: Editor, pos: number): number | undefined {
  const $pos = editor.state.doc.resolve(pos);
  return $pos.depth >= 1 ? $pos.before(1) : undefined;
}

/** Inserts a new user block of kind at, and puts the cursor in it. */
export function insertUserBlock(editor: Editor, kind: UserBlockKind, at: InsertAt) {
  const { doc } = editor.state;
  let from: number;
  let to: number;
  if ("replace" in at) {
    from = at.replace;
    to = at.replace + (doc.nodeAt(at.replace)?.nodeSize ?? 0);
  } else {
    from = at.after + (doc.nodeAt(at.after)?.nodeSize ?? 0);
    to = from;
  }
  editor
    .chain()
    .insertContentAt({ from, to }, newUserBlock(kind), { updateSelection: false })
    .run();
  const inserted = editor.state.doc.nodeAt(from);
  let textPos: number | undefined;
  editor.state.doc.nodesBetween(from, from + (inserted?.nodeSize ?? 0), (node, pos) => {
    if (textPos === undefined && node.isTextblock) textPos = pos + 1;
    return textPos === undefined;
  });
  editor
    .chain()
    .focus()
    .setTextSelection(textPos ?? from)
    .run();
}

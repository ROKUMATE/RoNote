import { Extension } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { type EditorState, Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import type { Annotation, IrBlock, TextSelector } from "../api/schema";
import { anchor, type Range, selectorOf } from "./anchoring";
import { eachBlock } from "./walk";

/*
 * Highlights in the editor (docs/adr/0007-annotations-and-anchoring.md):
 * decorations, never marks, so the content stays the page's and a highlight
 * is not an edit. Every annotation is anchored in its block's current text
 * on each change, so highlights follow the words while the user types.
 */

/** What the editor needs of an annotation to draw it. */
export type Highlight = Pick<Annotation, "id" | "block_id" | "selector">;

/** Where each highlight lands now; a highlight missing from placed is unplaced. */
export type Placements = {
  highlights: readonly Highlight[];
  placed: ReadonlyMap<string, Range>;
  decorations: DecorationSet;
};

/** A highlightable block of the current document: a textblock and where it starts, or code. */
type Target = { node: PMNode; pos: number } | { code: string };

const highlightsKey = new PluginKey<Placements>("highlights");

/** The prose blocks highlights go in; code is drawn by its own view. */
const prose = new Set(["paragraph", "heading"]);

/** Every block of doc a highlight can land in, by id. */
function targets(doc: PMNode): Map<string, Target> {
  const found = new Map<string, Target>();
  const code = (block: IrBlock) => {
    eachBlock([block], (b) => {
      if (b.kind === "code") found.set(b.id, { code: b.text ?? "" });
    });
  };
  doc.descendants((node, pos) => {
    if (prose.has(node.type.name) && node.attrs.blockId) {
      found.set(node.attrs.blockId, { node, pos });
      return false;
    }
    if (node.type.name === "codeBlock" || node.type.name === "tabs") {
      code(node.attrs.block as IrBlock);
      return false;
    }
    return true;
  });
  return found;
}

function place(doc: PMNode, highlights: readonly Highlight[]): Placements {
  const blocks = targets(doc);
  const placed = new Map<string, Range>();
  const marks: Decoration[] = [];
  for (const h of highlights) {
    const target = blocks.get(h.block_id);
    if (!target) continue;
    const range = anchor(h.selector, "code" in target ? target.code : target.node.textContent);
    if (!range) continue;
    placed.set(h.id, range);
    if ("node" in target) {
      const start = target.pos + 1;
      marks.push(
        Decoration.inline(start + range.start, start + range.end, {
          nodeName: "mark",
          class: "hl",
          "data-annotation": h.id,
        }),
      );
    }
  }
  return { highlights, placed, decorations: DecorationSet.create(doc, marks) };
}

/** What a keyboard shortcut asks of the page; true when it was handled. */
type ShortcutHandler = (shortcut: "highlight" | "toolbar") => boolean;

declare module "@tiptap/core" {
  interface Storage {
    highlights: { onShortcut: ShortcutHandler | undefined };
  }
}

/**
 * Draws highlights and reports where they land. Mod+Shift+H highlights the
 * selection and Alt+F10 moves to the highlight toolbar, through the
 * handler the page puts in the extension's storage.
 */
export const Highlights = Extension.create({
  name: "highlights",
  addStorage: () => ({ onShortcut: undefined as ShortcutHandler | undefined }),
  addKeyboardShortcuts() {
    return {
      "Mod-Shift-h": () => this.storage.onShortcut?.("highlight") ?? false,
      "Alt-F10": () => this.storage.onShortcut?.("toolbar") ?? false,
    };
  },
  addProseMirrorPlugins() {
    return [
      new Plugin<Placements>({
        key: highlightsKey,
        state: {
          init: (_, state) => place(state.doc, []),
          apply: (tr, current) => {
            const highlights: readonly Highlight[] | undefined = tr.getMeta(highlightsKey);
            if (highlights) return place(tr.doc, highlights);
            return tr.docChanged ? place(tr.doc, current.highlights) : current;
          },
        },
        props: {
          decorations: (state) => highlightsKey.getState(state)?.decorations,
        },
      }),
    ];
  },
});

/** Gives the editor the document's highlights. */
export function setHighlights(view: EditorView, highlights: readonly Highlight[]) {
  view.dispatch(view.state.tr.setMeta(highlightsKey, highlights).setMeta("addToHistory", false));
}

/** Where every highlight lands in the editor's current text. */
export function placements(state: EditorState): Placements {
  const current = highlightsKey.getState(state);
  if (!current) throw new Error("the editor has no Highlights extension");
  return current;
}

/** Part of a selection inside one block: its range of the block's text. */
export type Segment = { blockId: string; selector: TextSelector };

/**
 * The selection cut at block boundaries: one segment per prose block it
 * covers part of, each with a selector of its own.
 */
export function selectionSegments(state: EditorState): Segment[] {
  const { from, to, empty } = state.selection;
  if (empty) return [];
  const segments: Segment[] = [];
  state.doc.nodesBetween(from, to, (node, pos) => {
    if (!prose.has(node.type.name)) return true;
    const start = Math.max(from, pos + 1) - pos - 1;
    const end = Math.min(to, pos + 1 + node.content.size) - pos - 1;
    if (node.attrs.blockId && end > start) {
      segments.push({
        blockId: node.attrs.blockId,
        selector: selectorOf(node.textContent, start, end),
      });
    }
    return false;
  });
  return segments;
}

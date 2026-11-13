import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { moveTo } from "./moveTo";

const landingKey = new PluginKey<DecorationSet>("landing");

/** How long the flash lasts; reader.css's animation matches it. */
const flashMs = 900;

/**
 * Marks the block a search result landed on, as a node decoration, so the
 * editor keeps the mark while it redraws (reader.css flashes it once). An
 * edit clears it, so the block doesn't flash again as it is retyped.
 */
export const Landing = Extension.create({
  name: "landing",
  addProseMirrorPlugins() {
    return [
      new Plugin<DecorationSet>({
        key: landingKey,
        state: {
          init: () => DecorationSet.empty,
          apply(tr, set) {
            const next: DecorationSet | undefined = tr.getMeta(landingKey);
            if (next) return next;
            return tr.docChanged ? DecorationSet.empty : set;
          },
        },
        props: { decorations: (state) => landingKey.getState(state) },
      }),
    ];
  },
});

/**
 * Moves to the top-level block with blockId, as a search result lands, and
 * flashes it once. False when the document has no such block.
 */
export function landOn(view: EditorView, blockId: string): boolean {
  let at: number | undefined;
  view.state.doc.forEach((node, pos) => {
    if (node.attrs.blockId === blockId) at = pos;
  });
  const node = at === undefined ? null : view.state.doc.nodeAt(at);
  const dom = at === undefined ? null : view.nodeDOM(at);
  if (at === undefined || !node || !(dom instanceof HTMLElement)) return false;
  moveTo(view, dom);
  const mark = Decoration.node(at, at + node.nodeSize, { class: "landed" });
  view.dispatch(view.state.tr.setMeta(landingKey, DecorationSet.create(view.state.doc, [mark])));
  setTimeout(() => {
    if (!view.isDestroyed) view.dispatch(view.state.tr.setMeta(landingKey, DecorationSet.empty));
  }, flashMs);
  return true;
}

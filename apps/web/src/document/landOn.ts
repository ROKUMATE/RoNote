import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { moveTo } from "./moveTo";

const landingKey = new PluginKey<DecorationSet>("landing");

/** How long the flash lasts; reader.css's animation matches it. */
const flashMs = 900;

/**
 * How long a landing keeps its block in view while the page settles, and
 * at most: the document's font arriving re-wraps every paragraph above the
 * block, and code coming into view renders taller than its placeholder.
 */
const settleMs = 2000;
const holdMaxMs = 5000;

/** What a reader does to take over the scrolling from a landing. */
const takeovers = ["wheel", "touchstart", "keydown", "pointerdown"] as const;

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

/** The top-level block with blockId: where it is, and its element. */
function find(view: EditorView, blockId: string) {
  let at: number | undefined;
  view.state.doc.forEach((node, pos) => {
    if (node.attrs.blockId === blockId) at = pos;
  });
  const node = at === undefined ? null : view.state.doc.nodeAt(at);
  const dom = at === undefined ? null : view.nodeDOM(at);
  return at === undefined || !node || !(dom instanceof HTMLElement) ? undefined : { at, node, dom };
}

/**
 * Keeps the block in view while the layout above it settles, scrolling it
 * back each frame it moves, until it has stayed put for a while, or the
 * reader scrolls, types or clicks.
 */
function holdInView(view: EditorView, blockId: string) {
  const start = performance.now();
  let want = find(view, blockId)?.dom.getBoundingClientRect().top;
  let still = performance.now();
  let stopped = false;
  const stop = () => {
    stopped = true;
    for (const type of takeovers) window.removeEventListener(type, stop, { capture: true });
  };
  for (const type of takeovers)
    window.addEventListener(type, stop, { capture: true, passive: true });
  const frame = (now: number) => {
    const block = view.isDestroyed ? undefined : find(view, blockId);
    if (stopped || !block || want === undefined) return stop();
    if (Math.abs(block.dom.getBoundingClientRect().top - want) > 2) {
      block.dom.scrollIntoView();
      want = block.dom.getBoundingClientRect().top;
      still = now;
    }
    const settled = now - still > settleMs && document.fonts.status === "loaded";
    if (settled || now - start > holdMaxMs) return stop();
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
}

/**
 * Moves to the top-level block with blockId, as a search result lands, holds
 * it in view while the page settles, and flashes it once. False when the
 * document has no such block.
 */
export function landOn(view: EditorView, blockId: string): boolean {
  const found = find(view, blockId);
  if (!found) return false;
  const { at, node, dom } = found;
  moveTo(view, dom);
  holdInView(view, blockId);
  const mark = Decoration.node(at, at + node.nodeSize, { class: "landed" });
  view.dispatch(view.state.tr.setMeta(landingKey, DecorationSet.create(view.state.doc, [mark])));
  setTimeout(() => {
    if (!view.isDestroyed) view.dispatch(view.state.tr.setMeta(landingKey, DecorationSet.empty));
  }, flashMs);
  return true;
}

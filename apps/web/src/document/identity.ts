import { Extension } from "@tiptap/core";
import { Fragment, type Node as PMNode, Slice } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { kindOfNode } from "./schema";
import { uuidv7 } from "./uuid";

/*
 * Block identity while editing (docs/adr/0006-editor-and-doc-content.md):
 * - a top-level source block the user changes becomes edited;
 * - every block has an id, unique in the document. When a split copies an
 *   id, the new half gets a fresh one, and at the top level becomes a user
 *   block: only the original half stays the page's.
 * - code, tables, tabs and images are never pasted: they only come from the
 *   page, where each has its place.
 * An edited source block gets a widget before it: an "edited" label and a
 * "Revert to source" button.
 */

/** Marks a transaction that puts a source block back; it is not an edit. */
export const revertMeta = "ronote-revert";

const blockTypes = new Set(Object.keys(kindOfNode));
const lockedTypes = new Set(["codeBlock", "table", "tabs", "image"]);

/** Removes locked nodes from pasted content, wherever they are nested. */
function withoutLocked(fragment: Fragment): Fragment {
  const kept: PMNode[] = [];
  fragment.forEach((node) => {
    if (!lockedTypes.has(node.type.name)) kept.push(node.copy(withoutLocked(node.content)));
  });
  return Fragment.fromArray(kept);
}

function editedWidget(onRevert: (blockId: string) => void, blockId: string) {
  return () => {
    const mark = document.createElement("div");
    mark.className = "edited-mark";
    mark.contentEditable = "false";
    const label = document.createElement("span");
    label.textContent = "edited";
    const revert = document.createElement("button");
    revert.type = "button";
    revert.textContent = "Revert to source";
    revert.addEventListener("click", () => onRevert(blockId));
    mark.append(label, revert);
    return mark;
  };
}

const identityKey = new PluginKey<Map<string, PMNode>>("blockIdentity");

/** originals, plus every unedited top-level source block of doc not in it yet. */
function uneditedSources(doc: PMNode, originals: Map<string, PMNode>): Map<string, PMNode> {
  const next = new Map(originals);
  doc.forEach((node) => {
    const { blockId, origin, edited } = node.attrs;
    if (origin === "source" && !edited && blockId && !next.has(blockId)) next.set(blockId, node);
  });
  return next;
}

/** Whether a and b hold the same block, whatever their edited flags say. */
function sameBlock(a: PMNode, b: PMNode): boolean {
  return (
    a.type === b.type &&
    a.content.eq(b.content) &&
    JSON.stringify({ ...a.attrs, edited: null }) === JSON.stringify({ ...b.attrs, edited: null })
  );
}

export const BlockIdentityPlugin = Extension.create<{ onRevert: (blockId: string) => void }>({
  name: "blockIdentityPlugin",
  addOptions() {
    return { onRevert: () => {} };
  },
  addProseMirrorPlugins() {
    const { onRevert } = this.options;
    return [
      new Plugin<Map<string, PMNode>>({
        key: identityKey,
        state: {
          init: (_config, state) => uneditedSources(state.doc, new Map()),
          apply: (tr, originals) =>
            tr.getMeta(revertMeta) ? uneditedSources(tr.doc, originals) : originals,
        },
        appendTransaction(transactions, _oldState, newState) {
          if (!transactions.some((tr) => tr.docChanged)) return null;
          const originals = identityKey.getState(newState) ?? new Map<string, PMNode>();
          const tr = newState.tr;
          const seen = new Set<string>();
          newState.doc.descendants((node, pos, parent) => {
            if (!blockTypes.has(node.type.name)) return true;
            const top = parent === newState.doc;
            let attrs = node.attrs;
            const id: string | null = attrs.blockId;
            if (!id || seen.has(id)) {
              const fresh = uuidv7();
              attrs = top
                ? { ...attrs, blockId: fresh, origin: "user", edited: null, sourceBlockId: null }
                : { ...attrs, blockId: fresh };
            } else if (top && attrs.origin === "source") {
              // Edited means not as loaded. A block that loaded edited has
              // no original here, and stays edited until reverted.
              const original = originals.get(id);
              const edited = !original || !sameBlock(original, node);
              if (edited !== Boolean(attrs.edited)) attrs = { ...attrs, edited };
            }
            seen.add(attrs.blockId);
            if (attrs !== node.attrs) tr.setNodeMarkup(pos, undefined, attrs);
            return true;
          });
          return tr.docChanged ? tr : null;
        },
        props: {
          transformPasted: (slice) =>
            new Slice(withoutLocked(slice.content), slice.openStart, slice.openEnd),
          decorations(state) {
            const widgets: Decoration[] = [];
            state.doc.forEach((node, pos) => {
              if (node.attrs.origin === "source" && node.attrs.edited) {
                widgets.push(
                  Decoration.widget(pos, editedWidget(onRevert, node.attrs.blockId), {
                    side: -1,
                    key: `edited-${node.attrs.blockId}`,
                    ignoreSelection: true,
                    stopEvent: () => true,
                  }),
                );
              }
            });
            return DecorationSet.create(state.doc, widgets);
          },
        },
      }),
    ];
  },
});

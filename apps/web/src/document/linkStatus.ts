import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { type LinkTargets, linkStatus } from "./links";

const linkStatusKey = new PluginKey<LinkTargets | undefined>("linkStatus");

/**
 * Marks same-domain links in the text by where they lead: data-link is
 * "internal" for a page in the library and "import" for one not imported
 * yet. The marks stay as the page had them; this is decoration only.
 */
export const LinkStatus = Extension.create({
  name: "linkStatus",
  addProseMirrorPlugins() {
    return [
      new Plugin<LinkTargets | undefined>({
        key: linkStatusKey,
        state: {
          init: () => undefined,
          apply: (tr, targets) => tr.getMeta(linkStatusKey) ?? targets,
        },
        props: {
          decorations(state) {
            const targets = linkStatusKey.getState(state);
            if (!targets) return DecorationSet.empty;
            const marks: Decoration[] = [];
            state.doc.descendants((node, pos) => {
              if (!node.isText) return true;
              const link = node.marks.find(
                (m) => m.type.name === "link" && m.attrs.kind === "same_domain",
              );
              const status = link && linkStatus(link.attrs.href, targets);
              if (status && status.state !== "unknown") {
                marks.push(
                  Decoration.inline(pos, pos + node.nodeSize, { "data-link": status.state }),
                );
              }
              return false;
            });
            return DecorationSet.create(state.doc, marks);
          },
        },
      }),
    ];
  },
});

/** Tells the editor where its same-domain links lead. */
export function setLinkTargets(view: EditorView, targets: LinkTargets) {
  view.dispatch(view.state.tr.setMeta(linkStatusKey, targets).setMeta("addToHistory", false));
}

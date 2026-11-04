import type { Node } from "@tiptap/core";
import { UndoRedo } from "@tiptap/extensions";
import {
  NodeViewContent,
  type NodeViewProps,
  NodeViewWrapper,
  ReactNodeViewRenderer,
} from "@tiptap/react";
import type { IrBlock } from "../api/schema";
import { Block } from "./render";
import { atomNodes, extensions } from "./schema";

/** A block the editor shows whole: code, a table, tabs, an image or a divider. */
function AtomView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper contentEditable={false} className="atom">
      <Block block={node.attrs.block as IrBlock} />
    </NodeViewWrapper>
  );
}

/** A callout: its title is the page's and stays fixed; its body is editable. */
function CalloutView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper as="aside" className="callout" data-variant={node.attrs.variant}>
      {node.attrs.title && (
        <p className="callout-title" contentEditable={false}>
          {node.attrs.title}
        </p>
      )}
      <NodeViewContent />
    </NodeViewWrapper>
  );
}

/**
 * The schema with React views. Events inside an atom stay with its own
 * controls (Copy, tabs, scrolling), never with the editor.
 */
export const readerExtensions = [
  ...extensions.map((ext) => {
    if ((atomNodes as readonly string[]).includes(ext.name)) {
      return (ext as Node).extend({
        addNodeView: () => ReactNodeViewRenderer(AtomView, { stopEvent: () => true }),
      });
    }
    if (ext.name === "callout") {
      return (ext as Node).extend({ addNodeView: () => ReactNodeViewRenderer(CalloutView) });
    }
    return ext;
  }),
  UndoRedo,
];

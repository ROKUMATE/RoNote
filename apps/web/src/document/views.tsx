import type { Node } from "@tiptap/core";
import { UndoRedo } from "@tiptap/extensions/undo-redo";
import {
  NodeViewContent,
  type NodeViewProps,
  NodeViewWrapper,
  ReactNodeViewRenderer,
} from "@tiptap/react";
import type { IrBlock } from "../api/schema";
import { lineCount } from "../ui/CodeBlock";
import { Block } from "./render";
import { atomNodes, extensions } from "./schema";

/**
 * What reader.css needs to guess a block's height before it is drawn
 * (content-visibility): code's lines and a table's rows.
 */
function sizeHint(block: IrBlock): Record<string, string> {
  if (block.kind === "code") return { style: `--lines: ${lineCount(block.text ?? "")}` };
  if (block.kind === "table") {
    return { style: `--rows: ${(block.head?.length ?? 0) + (block.rows?.length ?? 0)}` };
  }
  return {};
}

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
    <NodeViewWrapper
      as="aside"
      className="callout"
      data-variant={node.attrs.variant}
      data-edited={node.attrs.origin === "source" && node.attrs.edited ? "true" : undefined}
    >
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
        addNodeView: () =>
          ReactNodeViewRenderer(AtomView, {
            stopEvent: () => true,
            attrs: ({ node }) => sizeHint(node.attrs.block as IrBlock),
          }),
      });
    }
    if (ext.name === "callout") {
      return (ext as Node).extend({ addNodeView: () => ReactNodeViewRenderer(CalloutView) });
    }
    return ext;
  }),
  UndoRedo,
];

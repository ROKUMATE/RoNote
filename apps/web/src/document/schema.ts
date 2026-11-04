import { Extension, Mark, Node } from "@tiptap/core";
import Blockquote from "@tiptap/extension-blockquote";
import Bold from "@tiptap/extension-bold";
import Code from "@tiptap/extension-code";
import Document from "@tiptap/extension-document";
import Heading from "@tiptap/extension-heading";
import Italic from "@tiptap/extension-italic";
import { ListItem } from "@tiptap/extension-list";
import Paragraph from "@tiptap/extension-paragraph";
import Strike from "@tiptap/extension-strike";
import Subscript from "@tiptap/extension-subscript";
import Superscript from "@tiptap/extension-superscript";
import Text from "@tiptap/extension-text";

/*
 * The editor's schema: one node per IR block kind and one mark per IR mark
 * (docs/adr/0006-editor-and-doc-content.md). Paragraphs, headings, lists,
 * quotes and callouts are editable. Code, tables, tabs, images and dividers
 * are atoms that carry their IR block whole; they have no parse rules, so
 * pasting can never make one.
 */

/** The IR kind each node type stands for. */
export const kindOfNode = {
  paragraph: "paragraph",
  heading: "heading",
  list: "list",
  blockquote: "quote",
  callout: "callout",
  codeBlock: "code",
  table: "table",
  tabs: "tabs",
  image: "image",
  divider: "divider",
} as const;

export type NodeName = keyof typeof kindOfNode;

/** The node types that hold an IR block whole and cannot be edited inline. */
export const atomNodes = ["codeBlock", "table", "tabs", "image", "divider"] as const;

/** The mark type each IR mark becomes. */
export const markOfIR = {
  bold: "bold",
  italic: "italic",
  code: "code",
  kbd: "kbd",
  strike: "strike",
  sub: "subscript",
  sup: "superscript",
} as const;

/**
 * Attributes every block node carries: its IR id, and at the top level
 * where it came from.
 */
const BlockIdentity = Extension.create({
  name: "blockIdentity",
  addGlobalAttributes() {
    return [
      {
        types: Object.keys(kindOfNode),
        attributes: {
          blockId: {
            default: null,
            rendered: true,
            renderHTML: (attrs) => (attrs.blockId ? { "data-block-id": attrs.blockId } : {}),
            parseHTML: () => null,
          },
          origin: { default: null, rendered: false },
          edited: { default: null, rendered: false },
          sourceBlockId: { default: null, rendered: false },
        },
      },
    ];
  },
});

const HeadingWithAnchor = Heading.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      anchor: {
        default: null,
        renderHTML: (attrs) => (attrs.anchor ? { id: attrs.anchor } : {}),
        parseHTML: () => null,
      },
    };
  },
});

/** A list, ordered or not, as one node like the IR's. */
const List = Node.create({
  name: "list",
  group: "block",
  content: "listItem+",
  addAttributes() {
    return { ordered: { default: false, rendered: false } };
  },
  renderHTML({ node, HTMLAttributes }) {
    return [node.attrs.ordered ? "ol" : "ul", HTMLAttributes, 0];
  },
});

/** A list item holds any blocks; IR items may start with code. */
const Item = ListItem.extend({ content: "block+" });

const Callout = Node.create({
  name: "callout",
  group: "block",
  content: "block*",
  defining: true,
  addAttributes() {
    return {
      variant: { default: "note", rendered: false },
      title: { default: null, rendered: false },
    };
  },
  renderHTML({ node, HTMLAttributes }) {
    return ["aside", { ...HTMLAttributes, "data-variant": node.attrs.variant }, 0];
  },
});

const Kbd = Mark.create({
  name: "kbd",
  parseHTML: () => [{ tag: "kbd" }],
  renderHTML: () => ["kbd", 0],
});

/**
 * A link with the IR's kind: anchor, same_domain or external. Pasted links
 * are kept only when they are http(s) or a #fragment, as the IR allows.
 */
const IRLink = Mark.create({
  name: "link",
  inclusive: false,
  addAttributes() {
    return { href: { default: null }, kind: { default: "external" } };
  },
  parseHTML() {
    return [
      {
        tag: "a[href]",
        getAttrs: (el) => {
          const href = el.getAttribute("href") ?? "";
          if (href.startsWith("#") && href.length > 1) return { href, kind: "anchor" };
          return /^https?:\/\//i.test(href) ? { href, kind: "external" } : false;
        },
      },
    ];
  },
  renderHTML({ mark }) {
    return mark.attrs.kind === "anchor"
      ? ["a", { href: mark.attrs.href }, 0]
      : ["a", { href: mark.attrs.href, target: "_blank", rel: "noopener noreferrer" }, 0];
  },
});

/** A block the editor shows but never edits; its IR block is its data. */
function atom(name: (typeof atomNodes)[number], tag: string) {
  return Node.create({
    name,
    group: "block",
    atom: true,
    selectable: true,
    draggable: false,
    addAttributes() {
      return { block: { default: null, rendered: false } };
    },
    renderHTML: ({ HTMLAttributes }) => [tag, HTMLAttributes],
  });
}

export const extensions = [
  Document,
  Text,
  Paragraph,
  HeadingWithAnchor,
  List,
  Item,
  Blockquote,
  Callout,
  atom("codeBlock", "pre"),
  atom("table", "table"),
  atom("tabs", "div"),
  atom("image", "figure"),
  atom("divider", "hr"),
  Bold,
  Italic,
  Code,
  Kbd,
  Strike,
  Subscript,
  Superscript,
  IRLink,
  BlockIdentity,
];

import type { JSONContent } from "@tiptap/core";
import type { DocBlock, DocContent, IrBlock, IrInline } from "../api/schema";
import { atomNodes, kindOfNode, markOfIR, type NodeName } from "./schema";
import { uuidv7 } from "./uuid";

/*
 * IR ⇄ editor JSON, the one place that knows both shapes
 * (docs/adr/0006-editor-and-doc-content.md).
 */

type Kind = IrBlock["kind"];
type IRMark = NonNullable<IrInline["marks"]>[number];

const nodeOfKind = Object.fromEntries(
  Object.entries(kindOfNode).map(([node, kind]) => [kind, node]),
) as Record<Kind, NodeName>;

/** IR marks in the order the IR lists them, which output keeps. */
const irMarks = Object.keys(markOfIR) as IRMark[];

/** The document as editor JSON. */
export function toEditor(content: DocContent): JSONContent {
  return {
    type: "doc",
    content: content.blocks.map((b) => {
      const node = blockNode(b);
      return {
        ...node,
        attrs: {
          ...node.attrs,
          origin: b.origin,
          edited: b.origin === "source" ? (b.edited ?? false) : null,
          sourceBlockId: b.source_block_id ?? null,
        },
      };
    }),
  };
}

function blockNode(b: IrBlock): JSONContent {
  const type = nodeOfKind[b.kind];
  const id = { blockId: b.id };
  if ((atomNodes as readonly string[]).includes(type)) {
    const { origin: _o, edited: _e, source_block_id: _s, ...block } = b as DocBlock;
    return { type, attrs: { ...id, block } };
  }
  switch (b.kind) {
    case "heading":
      return {
        type,
        attrs: { ...id, level: b.level, anchor: b.anchor ?? null },
        content: inlines(b.content),
      };
    case "list":
      return {
        type,
        attrs: { ...id, ordered: b.ordered ?? false },
        content: (b.items ?? []).map((item) => ({
          type: "listItem",
          content: item.children.map(blockNode),
        })),
      };
    case "callout":
      return {
        type,
        attrs: { ...id, variant: b.variant, title: b.title ?? null },
        content: (b.children ?? []).map(blockNode),
      };
    case "quote":
      return { type, attrs: id, content: (b.children ?? []).map(blockNode) };
    default:
      return { type, attrs: id, content: inlines(b.content) };
  }
}

function inlines(runs: IrInline[] | undefined): JSONContent[] {
  return (runs ?? []).map((run) => {
    const marks: JSONContent["marks"] = (run.marks ?? []).map((m) => ({ type: markOfIR[m] }));
    if (run.link) marks.push({ type: "link", attrs: { href: run.link.href, kind: run.link.kind } });
    return { type: "text", text: run.text, ...(marks.length ? { marks } : {}) };
  });
}

/**
 * The editor's document as DocContent. A source block the user has not
 * edited is taken from originals exactly as loaded, so the editor's
 * normalizing never touches it; everything else is read from the editor.
 * Blocks left empty by editing are dropped.
 */
export function fromEditor(doc: JSONContent, originals: ReadonlyMap<string, DocBlock>): DocContent {
  const blocks: DocBlock[] = [];
  for (const node of doc.content ?? []) {
    const attrs = node.attrs ?? {};
    const original = originals.get(attrs.blockId);
    if (attrs.origin === "source" && !attrs.edited && original) {
      blocks.push(original);
      continue;
    }
    const block = irBlock(node);
    if (!block) continue;
    blocks.push(
      attrs.origin === "source"
        ? {
            ...block,
            origin: "source",
            edited: Boolean(attrs.edited),
            source_block_id: attrs.sourceBlockId,
          }
        : { ...block, id: isUUID(block.id) ? block.id : uuidv7(), origin: "user" },
    );
  }
  return { doc_version: 1, blocks };
}

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function isUUID(id: string) {
  return uuidPattern.test(id);
}

/** The IR block a node holds, or undefined if editing left it empty. */
function irBlock(node: JSONContent): IrBlock | undefined {
  const type = node.type as NodeName;
  const attrs = node.attrs ?? {};
  const id: string = attrs.blockId ?? uuidv7();
  if ((atomNodes as readonly string[]).includes(type)) {
    return { ...(attrs.block as IrBlock), id };
  }
  const kind = kindOfNode[type];
  const children = () => blocksOf(node.content);
  switch (kind) {
    case "paragraph":
    case "heading": {
      const content = runs(node.content);
      if (content.length === 0) return undefined;
      return kind === "heading"
        ? {
            id,
            kind,
            level: attrs.level,
            ...(attrs.anchor ? { anchor: attrs.anchor } : {}),
            content,
          }
        : { id, kind, content };
    }
    case "list": {
      const items = (node.content ?? [])
        .map((item) => ({ children: blocksOf(item.content) }))
        .filter((item) => item.children.length > 0);
      if (items.length === 0) return undefined;
      return { id, kind, ...(attrs.ordered ? { ordered: true } : {}), items };
    }
    case "quote": {
      const content = children();
      return content.length ? { id, kind, children: content } : undefined;
    }
    case "callout": {
      const content = children();
      if (!attrs.title && content.length === 0) return undefined;
      return {
        id,
        kind,
        variant: attrs.variant,
        ...(attrs.title ? { title: attrs.title } : {}),
        ...(content.length ? { children: content } : {}),
      };
    }
    default:
      return undefined;
  }
}

function blocksOf(nodes: JSONContent[] | undefined): IrBlock[] {
  return (nodes ?? []).flatMap((n) => irBlock(n) ?? []);
}

/** Text nodes as IR runs: marks in IR order, equal neighbours merged. */
function runs(nodes: JSONContent[] | undefined): IrInline[] {
  const out: IrInline[] = [];
  for (const node of nodes ?? []) {
    if (node.type !== "text" || !node.text) continue;
    const marks = irMarks.filter((m) => node.marks?.some((mark) => mark.type === markOfIR[m]));
    const link = node.marks?.find((mark) => mark.type === "link")?.attrs;
    const run: IrInline = {
      text: node.text,
      ...(marks.length ? { marks } : {}),
      ...(link ? { link: { href: link.href, kind: link.kind } } : {}),
    };
    const last = out.at(-1);
    if (last && sameStyle(last, run)) {
      last.text += run.text;
    } else {
      out.push(run);
    }
  }
  return out;
}

function sameStyle(a: IrInline, b: IrInline) {
  return (
    JSON.stringify(a.marks ?? []) === JSON.stringify(b.marks ?? []) &&
    JSON.stringify(a.link ?? null) === JSON.stringify(b.link ?? null)
  );
}

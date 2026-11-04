import type { IrBlock } from "../api/schema";
import { eachBlock } from "./walk";

type Kind = IrBlock["kind"];

/** How each kind is named in a count, singular and plural, in display order. */
const kindNames: Record<Kind, readonly [string, string]> = {
  heading: ["heading", "headings"],
  paragraph: ["paragraph", "paragraphs"],
  list: ["list", "lists"],
  code: ["code block", "code blocks"],
  table: ["table", "tables"],
  callout: ["callout", "callouts"],
  tabs: ["tab group", "tab groups"],
  image: ["image", "images"],
  quote: ["quote", "quotes"],
  divider: ["divider", "dividers"],
};

/**
 * Counts a document's blocks by kind, the ones inside lists, tabs and
 * callouts included, as phrases such as "153 code blocks". Kinds the
 * document does not have are left out.
 */
export function blockCounts(blocks: readonly IrBlock[]): string[] {
  const counts = new Map<Kind, number>();
  eachBlock(blocks, (block) => counts.set(block.kind, (counts.get(block.kind) ?? 0) + 1));
  return Object.entries(kindNames).flatMap(([kind, [one, many]]) => {
    const n = counts.get(kind as Kind) ?? 0;
    return n === 0 ? [] : [`${n} ${n === 1 ? one : many}`];
  });
}

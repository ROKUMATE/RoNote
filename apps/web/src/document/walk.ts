import type { IrBlock } from "../api/schema";

/** Calls visit on every block in blocks, and on the blocks nested in lists, quotes, callouts and tabs. */
export function eachBlock(blocks: readonly IrBlock[], visit: (block: IrBlock) => void) {
  for (const block of blocks) {
    visit(block);
    eachBlock(block.children ?? [], visit);
    for (const item of block.items ?? []) eachBlock(item.children, visit);
  }
}

/*
 * Where a reader was in a document, kept by block rather than by pixels:
 * blocks the browser has not drawn yet (content-visibility) and parts of
 * the page that arrive late change the pixels above, but not which block
 * was at the top of the viewport, or how far down it sat.
 */

/** The block at the top of the viewport, and how far its top is from the viewport's. */
export type Anchor = { blockId: string; offset: number };

/** The first of blocks, in document order, still showing at the top of the viewport. */
export function topAnchor(
  blocks: Iterable<{ blockId: string; element: Element }>,
): Anchor | undefined {
  for (const { blockId, element } of blocks) {
    const box = element.getBoundingClientRect();
    if (box.bottom > 0) return { blockId, offset: box.top };
  }
  return undefined;
}

/** What restoring needs of the page; the hook gives it the browser's. */
export type RestoreEnv = {
  /** Where the block's top is now in the viewport, once it is drawn. */
  top: (blockId: string) => number | undefined;
  scrollBy: (dy: number) => void;
  /** Calls back when the page's layout changes size; returns a stop function. */
  onLayout: (changed: () => void) => () => void;
  /** Calls back when the reader starts scrolling themselves; returns a stop function. */
  onUserScroll: (scrolled: () => void) => () => void;
  after: (ms: number, run: () => void) => () => void;
};

/** How long after coming back a change above the anchor is put right. */
const settleMs = 1000;

/**
 * Brings anchor's block back to where it was, once it is drawn. If the
 * layout above it changes within the first second, as when part of the page
 * arrives late, the block is put back once more; then restoring stops. It
 * also stops for good the moment the reader scrolls: their scrolling wins.
 * Returns a function that stops it early.
 */
export function restoreAnchor(anchor: Anchor, env: RestoreEnv): () => void {
  let applied = 0;
  const stops: (() => void)[] = [];
  const stop = () => {
    for (const s of stops.splice(0)) s();
  };
  /** Moves the block to its offset; false while it is not drawn yet. */
  const apply = () => {
    const top = env.top(anchor.blockId);
    if (top === undefined) return false;
    const dy = top - anchor.offset;
    if (Math.abs(dy) >= 1) env.scrollBy(dy);
    applied++;
    return true;
  };

  stops.push(env.onUserScroll(stop));
  stops.push(env.after(settleMs, stop));
  stops.push(
    env.onLayout(() => {
      if (applied === 0) {
        apply();
        return;
      }
      const top = env.top(anchor.blockId);
      if (top !== undefined && Math.abs(top - anchor.offset) >= 1) {
        apply();
        stop();
      }
    }),
  );
  apply();
  return stop;
}

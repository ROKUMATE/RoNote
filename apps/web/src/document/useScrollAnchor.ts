import { useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";

/** Where a page was read: its top-level block at the top of the viewport. */
type Anchor = { index: number; offset: number };

const anchors = new Map<string, Anchor>();

function blocks(): HTMLCollection | undefined {
  return document.querySelector(".reader-content")?.children;
}

/**
 * Returns to where the reader was when they come back to a document, by
 * the block at the top of the viewport rather than by pixels: blocks the
 * browser skips rendering (content-visibility) take a placeholder height
 * until shown, so the same pixel offset would land elsewhere. Kept per
 * history entry, so back and forward each find their own place.
 */
export function useScrollAnchor(ready: boolean) {
  const key = useRouterState({ select: (s) => s.location.state.__TSR_key ?? s.location.href });

  useEffect(() => {
    if (!ready) return;
    const saved = anchors.get(key);
    let frame = requestAnimationFrame(() => {
      // After the router's own restore, which runs first.
      frame = requestAnimationFrame(() => {
        const block = saved && blocks()?.[saved.index];
        if (block) window.scrollBy(0, block.getBoundingClientRect().top - saved.offset);
      });
    });
    let pending = 0;
    const record = () => {
      pending ||= requestAnimationFrame(() => {
        pending = 0;
        const list = [...(blocks() ?? [])];
        const index = list.findIndex((b) => b.getBoundingClientRect().bottom > 0);
        if (index >= 0)
          anchors.set(key, { index, offset: list[index]?.getBoundingClientRect().top ?? 0 });
      });
    };
    window.addEventListener("scroll", record, { passive: true });
    return () => {
      cancelAnimationFrame(frame);
      cancelAnimationFrame(pending);
      window.removeEventListener("scroll", record);
    };
  }, [key, ready]);
}

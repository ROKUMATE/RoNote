import { useRouterState } from "@tanstack/react-router";
import type { EditorView } from "@tiptap/pm/view";
import { useEffect } from "react";
import { moveTo } from "./moveTo";

/** Where a page was read: its top-level block at the top of the viewport. */
type Anchor = { index: number; offset: number };

const anchors = new Map<string, Anchor>();

function blocks(): HTMLCollection | undefined {
  return document.querySelector(".reader-content")?.children;
}

/**
 * Where a document opens:
 * - coming back to it (back, forward), where the reader was, found by the
 *   block at the top of the viewport rather than by pixels: blocks the
 *   browser skips rendering (content-visibility) take a placeholder height
 *   until shown, so the same pixel offset would land elsewhere;
 * - arriving by a link with a #fragment, at that heading, with the caret
 *   in it.
 * Places are kept per history entry, so back and forward each find their own.
 */
export function useScrollAnchor(view: EditorView | undefined) {
  const key = useRouterState({ select: (s) => s.location.state.__TSR_key ?? s.location.href });
  const hash = useRouterState({ select: (s) => s.location.hash });

  useEffect(() => {
    if (!view) return;
    const saved = anchors.get(key);
    let frame = requestAnimationFrame(() => {
      // After the router's own restore, which runs first.
      frame = requestAnimationFrame(() => {
        const block = saved && blocks()?.[saved.index];
        const heading = !saved && hash ? document.getElementById(decodeURIComponent(hash)) : null;
        if (block) {
          window.scrollBy(0, block.getBoundingClientRect().top - saved.offset);
        } else if (heading && view.dom.contains(heading)) {
          moveTo(view, heading);
        }
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
  }, [key, hash, view]);
}

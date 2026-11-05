import { useRouter, useRouterState } from "@tanstack/react-router";
import type { EditorView } from "@tiptap/pm/view";
import { useEffect } from "react";
import { moveTo } from "./moveTo";
import { type Anchor, type RestoreEnv, restoreAnchor, topAnchor } from "./scrollAnchor";

/** Where each history entry's document was being read, kept for reloads too. */
const storageKey = "ronote.reading-places";
/** How many places sessionStorage keeps: the newest. */
const keep = 100;

const places = load();

function load(): Map<string, Anchor> {
  try {
    return new Map(JSON.parse(sessionStorage.getItem(storageKey) ?? "[]"));
  } catch {
    return new Map();
  }
}

function persist() {
  try {
    sessionStorage.setItem(storageKey, JSON.stringify([...places].slice(-keep)));
  } catch {
    // Without storage, places last until the tab is reloaded.
  }
}

/** The document's top-level blocks as drawn, in order. */
function blocksOf(view: EditorView): { blockId: string; element: Element }[] {
  const blocks: { blockId: string; element: Element }[] = [];
  view.state.doc.forEach((node, offset) => {
    const element = view.nodeDOM(offset);
    if (element instanceof Element && node.attrs.blockId) {
      blocks.push({ blockId: node.attrs.blockId, element });
    }
  });
  return blocks;
}

/** Input that means the reader is scrolling, or about to, themselves. */
const userInput = ["wheel", "touchmove", "keydown", "pointerdown"] as const;

function browser(view: EditorView): RestoreEnv {
  return {
    top: (blockId) =>
      blocksOf(view)
        .find((b) => b.blockId === blockId)
        ?.element.getBoundingClientRect().top,
    scrollBy: (dy) => window.scrollBy(0, dy),
    onLayout: (changed) => {
      const observer = new ResizeObserver(() => changed());
      observer.observe(document.body);
      observer.observe(view.dom);
      return () => observer.disconnect();
    },
    onUserScroll: (scrolled) => {
      for (const type of userInput) window.addEventListener(type, scrolled, { passive: true });
      return () => {
        for (const type of userInput) window.removeEventListener(type, scrolled);
      };
    },
    after: (ms, run) => {
      const timer = setTimeout(run, ms);
      return () => clearTimeout(timer);
    },
  };
}

/**
 * Where a document opens, owned here rather than by the router, whose
 * pixel restore would land elsewhere whenever the layout above differs:
 * - coming back to it (back, forward, reload), the block that was at the
 *   top of the viewport, at the same offset (scrollAnchor.ts);
 * - arriving by a link with a #fragment, at that heading, with the caret
 *   in it;
 * - otherwise at the top.
 * Places are kept per history entry, so back and forward each find their own.
 */
export function useScrollAnchor(view: EditorView | undefined) {
  const router = useRouter();
  const key = useRouterState({ select: (s) => s.location.state.__TSR_key ?? s.location.href });
  const hash = useRouterState({ select: (s) => s.location.hash });

  useEffect(() => {
    const saved = places.get(key);
    if (!view) {
      if (!saved && !hash) window.scrollTo(0, 0);
      return;
    }
    let stopRestore = () => {};
    if (saved) {
      stopRestore = restoreAnchor(saved, browser(view));
    } else if (hash) {
      const heading = document.getElementById(decodeURIComponent(hash));
      if (heading && view.dom.contains(heading)) moveTo(view, heading);
    }

    const record = () => {
      const anchor = topAnchor(blocksOf(view));
      if (anchor) places.set(key, anchor);
    };
    let pending = 0;
    const onScroll = () => {
      pending ||= requestAnimationFrame(() => {
        pending = 0;
        record();
      });
    };
    // Leaving: the page as the reader last saw it, laid out still.
    const leave = () => {
      record();
      persist();
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", leave);
    const unsubscribe = router.subscribe("onBeforeNavigate", leave);
    return () => {
      stopRestore();
      cancelAnimationFrame(pending);
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("pagehide", leave);
      unsubscribe();
    };
  }, [router, key, hash, view]);
}

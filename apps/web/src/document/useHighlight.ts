import { type RefCallback, useCallback, useEffect, useState } from "react";
import type { ThemedToken } from "shiki/core";
import { languageOf, loadLanguage } from "./languages";

/** How near the viewport a code block is when its highlighting starts. */
const nearViewport = "600px";

const waiting = new Map<Element, () => void>();
let observer: IntersectionObserver | undefined;

/** Calls onNear once el comes within nearViewport of the viewport. */
function whenNear(el: Element, onNear: () => void): () => void {
  observer ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        waiting.get(entry.target)?.();
        waiting.delete(entry.target);
        observer?.unobserve(entry.target);
      }
    },
    { rootMargin: nearViewport },
  );
  waiting.set(el, onNear);
  observer.observe(el);
  return () => {
    waiting.delete(el);
    observer?.unobserve(el);
  };
}

/**
 * The code's highlighted tokens, once its block has come near the
 * viewport and its language has loaded; undefined until then, and for
 * languages RoNote has no grammar for.
 */
export function useHighlight(code: string, lang: string | undefined) {
  const [el, setEl] = useState<Element | null>(null);
  const [lines, setLines] = useState<ThemedToken[][]>();
  const ref: RefCallback<Element> = useCallback((node) => setEl(node), []);
  const language = languageOf(lang);

  useEffect(() => {
    if (!el || !language) return;
    let live = true;
    const stop = whenNear(el, () => {
      void Promise.all([import("./highlighter"), loadLanguage(language)])
        .then(([{ highlight }, grammar]) => highlight(code, language, grammar))
        .then((tokens) => {
          if (live) setLines(tokens);
        })
        // Highlighting is an extra: if a grammar fails, the plain text stays.
        .catch(() => {});
    });
    return () => {
      live = false;
      stop();
    };
  }, [el, code, language]);

  return { ref, lines };
}

import { NoteIcon } from "@phosphor-icons/react/Note";
import type { Editor } from "@tiptap/core";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { Annotation } from "../../api/schema";
import { notesSlotId } from "../../shell/AppShell";
import { stack } from "./stack";

/** Space between notes stacked in the margin. */
const gap = 8;

type Layout = { wide: boolean; tops: ReadonlyMap<string, number> };

function sameLayout(a: Layout, b: Layout) {
  return (
    a.wide === b.wide &&
    a.tops.size === b.tops.size &&
    [...a.tops].every(([id, top]) => Math.abs((b.tops.get(id) ?? Number.NaN) - top) < 0.5)
  );
}

/**
 * Notes beside their highlights (docs/adr/0007-annotations-and-anchoring.md).
 * Where the page has its notes margin (the shell's Notes aside, at the
 * notes breakpoint), each note sits there level with its highlight, pushed
 * down when the one above would overlap. Without it, a note icon sits just
 * right of the text, level with the highlight. Either opens the note.
 */
export function Notes({
  editor,
  notes,
  frame,
  onOpen,
}: {
  editor: Editor;
  notes: readonly Annotation[];
  frame: HTMLElement;
  onOpen: (id: string) => void;
}) {
  const [slot] = useState(() => document.getElementById(notesSlotId));
  const [layout, setLayout] = useState<Layout>({ wide: false, tops: new Map() });
  const elements = useRef(new Map<string, HTMLElement>());

  const measure = useCallback(() => {
    // The margin is there exactly when the shell shows its aside.
    const wide = slot !== null && slot.getClientRects().length > 0;
    const base = (wide && slot ? slot : frame).getBoundingClientRect().top;
    const placed = notes.flatMap((a) => {
      const mark = editor.view.dom.querySelector(`[data-annotation="${a.id}"]`);
      if (!mark) return [];
      const height = elements.current.get(a.id)?.offsetHeight ?? 0;
      return [{ id: a.id, top: mark.getBoundingClientRect().top - base, height }];
    });
    const next = { wide, tops: stack(placed, gap) };
    setLayout((old) => (sameLayout(old, next) ? old : next));
  }, [editor, notes, frame, slot]);

  // After every draw: heights of notes come from the draw.
  useLayoutEffect(measure);

  // Highlights move with edits, resizing, and blocks drawn for the first
  // time (content-visibility gives them a stand-in height until then).
  useEffect(() => {
    let frameId = 0;
    const soon = () => {
      frameId ||= requestAnimationFrame(() => {
        frameId = 0;
        measure();
      });
    };
    editor.on("transaction", soon);
    window.addEventListener("resize", soon);
    const resized = new ResizeObserver(soon);
    resized.observe(editor.view.dom);
    return () => {
      editor.off("transaction", soon);
      window.removeEventListener("resize", soon);
      resized.disconnect();
      cancelAnimationFrame(frameId);
    };
  }, [editor, measure]);

  const shown = notes.filter((a) => layout.tops.has(a.id));
  const keep = (id: string) => (el: HTMLElement | null) => {
    if (el) elements.current.set(id, el);
    else elements.current.delete(id);
  };

  if (layout.wide && slot) {
    return createPortal(
      <ol className="relative font-sans text-ink text-ui">
        {shown.map((a) => (
          <li
            key={a.id}
            ref={keep(a.id)}
            className="absolute inset-x-0"
            style={{ top: layout.tops.get(a.id) }}
          >
            <button
              type="button"
              onClick={() => onOpen(a.id)}
              className="w-full rounded-md border border-rule bg-paper px-3 py-2 text-left hover:bg-paper-2"
            >
              <span className="sr-only">Note on “{a.selector.quote.exact}”: </span>
              <span className="line-clamp-6 whitespace-pre-line">{a.note}</span>
            </button>
          </li>
        ))}
      </ol>,
      slot,
    );
  }
  return shown.map((a) => (
    <button
      key={a.id}
      ref={keep(a.id)}
      type="button"
      aria-label={`Note on “${a.selector.quote.exact}”`}
      onClick={() => onOpen(a.id)}
      style={{ top: layout.tops.get(a.id), left: frame.clientWidth + 2 }}
      className="absolute z-10 rounded-sm text-gen hover:text-ink"
    >
      <NoteIcon size={16} aria-hidden />
    </button>
  ));
}

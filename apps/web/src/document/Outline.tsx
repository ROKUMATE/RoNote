import type { Editor } from "@tiptap/core";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { outlineSlotId } from "../shell/AppShell";
import { moveTo } from "./moveTo";

type Entry = { id: string; anchor: string | null; level: 2 | 3; text: string };

/** The document's h2 and h3 headings, nested ones included. */
function headingsOf(editor: Editor): Entry[] {
  const out: Entry[] = [];
  editor.state.doc.descendants((node) => {
    const { level, blockId, anchor } = node.attrs;
    if (
      node.type.name === "heading" &&
      (level === 2 || level === 3) &&
      blockId &&
      node.textContent
    ) {
      out.push({ id: blockId, anchor, level, text: node.textContent });
    }
    return node.type.name !== "heading";
  });
  return out;
}

function headingElement(id: string): HTMLElement | null {
  return document.querySelector(`.reader-content [data-block-id="${CSS.escape(id)}"]`);
}

/**
 * The outline in the sidebar: the document's h2 and h3 headings, the one
 * being read marked as the current location.
 */
export function Outline({ editor }: { editor: Editor }) {
  const [entries, setEntries] = useState(() => headingsOf(editor));
  const [current, setCurrent] = useState<string>();
  const [slot, setSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    setSlot(document.getElementById(outlineSlotId));
    const update = () => setEntries(headingsOf(editor));
    editor.on("update", update);
    return () => {
      editor.off("update", update);
    };
  }, [editor]);

  // Scroll spy: the current heading is the last one above the line a third
  // of the way down the viewport. Measured on every scrolled frame, so a jump
  // past several headings still lands on the right one.
  useEffect(() => {
    let frame = 0;
    const measure = () => {
      frame = 0;
      const line = window.innerHeight / 3;
      let above: string | undefined;
      for (const entry of entries) {
        const top = headingElement(entry.id)?.getBoundingClientRect().top;
        if (top !== undefined && top <= line) above = entry.id;
      }
      setCurrent(above ?? entries[0]?.id);
    };
    const onScroll = () => {
      frame ||= requestAnimationFrame(measure);
    };
    measure();
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
      cancelAnimationFrame(frame);
    };
  }, [entries]);

  if (!slot || entries.length === 0) return null;
  return createPortal(
    <nav aria-label="Outline" className="mt-6 font-sans text-ui">
      <h2 className="px-2 text-ink-2">On this page</h2>
      <ul className="mt-1 space-y-0.5">
        {entries.map((entry) => (
          <li key={entry.id} className={entry.level === 3 ? "pl-3" : undefined}>
            <a
              href={`#${entry.anchor ?? ""}`}
              aria-current={entry.id === current ? "location" : undefined}
              onClick={(event) => {
                event.preventDefault();
                setCurrent(entry.id);
                const heading = headingElement(entry.id);
                if (heading) moveTo(editor.view, heading);
              }}
              className="block truncate rounded-sm px-2 py-0.5 text-ink-2 hover:bg-paper-2 hover:text-ink aria-[current=location]:text-ink aria-[current=location]:font-medium"
            >
              {entry.text}
            </a>
          </li>
        ))}
      </ul>
    </nav>,
    slot,
  );
}

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

  // Scroll spy: the current heading is the last one to have crossed into
  // the top third of the viewport.
  useEffect(() => {
    const observer = new IntersectionObserver(
      (records) => {
        const crossing = records
          .filter((r) => r.isIntersecting)
          .map((r) => r.target.getAttribute("data-block-id"));
        const last = crossing.at(-1);
        if (last) setCurrent(last);
      },
      { rootMargin: "0px 0px -66% 0px" },
    );
    for (const entry of entries) {
      const heading = headingElement(entry.id);
      if (heading) observer.observe(heading);
    }
    return () => observer.disconnect();
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

import type { Editor } from "@tiptap/core";
import { type ReactNode, useEffect, useId, useState } from "react";
import { createPortal } from "react-dom";
import type { Annotation } from "../../api/schema";
import { placements } from "../highlights";

/** How much of a quote the list shows. */
const quoteLength = 120;

function short(text: string) {
  return text.length > quoteLength ? `${text.slice(0, quoteLength)}…` : text;
}

const action =
  "rounded-sm px-1.5 py-0.5 text-ink-2 underline underline-offset-2 hover:bg-paper-2 hover:text-ink";

/**
 * Every highlight of the document, in the order of its text, with its
 * quote and note; then the unplaced ones, whose text changed past
 * recognising or went away (docs/adr/0007-annotations-and-anchoring.md).
 * Nothing is ever removed for the user: an unplaced highlight waits here
 * until it is removed or re-applied to new text.
 */
export function HighlightsList({
  editor,
  list,
  slot,
  onJump,
  onOpen,
  onRemove,
  onReapply,
}: {
  editor: Editor;
  list: readonly Annotation[];
  slot: HTMLElement;
  onJump: (id: string) => void;
  onOpen: (id: string) => void;
  onRemove: (annotation: Annotation) => void;
  onReapply: (annotation: Annotation) => void;
}) {
  const [placed, setPlaced] = useState(() => placements(editor.state));
  useEffect(() => {
    const update = () => setPlaced(placements(editor.state));
    update();
    editor.on("transaction", update);
    return () => {
      editor.off("transaction", update);
    };
  }, [editor]);
  const unplacedId = useId();

  const byId = new Map(list.map((a) => [a.id, a]));
  const inOrder = placed.order.flatMap((id) => byId.get(id) ?? []);
  // Only what the editor has been given can be judged unplaced.
  const unplaced = placed.highlights.flatMap((h) =>
    placed.placed.has(h.id) ? [] : (byId.get(h.id) ?? []),
  );

  const entry = (a: Annotation, actions: ReactNode) => (
    <li key={a.id} className="space-y-1 border-rule border-t py-2">
      <q className="block font-serif text-ink">{short(a.selector.quote.exact)}</q>
      {a.note && <p className="whitespace-pre-line text-ink-2">{a.note}</p>}
      <div className="flex flex-wrap gap-2">{actions}</div>
    </li>
  );

  return createPortal(
    <details className="font-sans text-ui">
      <summary className="cursor-pointer text-ink-2 hover:text-ink">
        Highlights ({list.length})
      </summary>
      {inOrder.length > 0 && (
        <ol className="mt-2">
          {inOrder.map((a) =>
            entry(
              a,
              <>
                <button type="button" onClick={() => onJump(a.id)} className={action}>
                  Jump to
                </button>
                <button type="button" onClick={() => onOpen(a.id)} className={action}>
                  {a.note ? "Edit note" : "Add note"}
                </button>
                <button type="button" onClick={() => onRemove(a)} className={action}>
                  Remove
                </button>
              </>,
            ),
          )}
        </ol>
      )}
      {unplaced.length > 0 && (
        <section aria-labelledby={unplacedId} className="mt-3">
          <h2 id={unplacedId} className="font-semibold text-ink">
            Unplaced highlights
          </h2>
          <p className="text-ink-2">
            Their text changed or went away. Re-apply one by selecting new text for it.
          </p>
          <ol className="mt-1">
            {unplaced.map((a) =>
              entry(
                a,
                <>
                  <button type="button" onClick={() => onReapply(a)} className={action}>
                    Re-apply
                  </button>
                  <button type="button" onClick={() => onRemove(a)} className={action}>
                    Remove
                  </button>
                </>,
              ),
            )}
          </ol>
        </section>
      )}
    </details>,
    slot,
  );
}

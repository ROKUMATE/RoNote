import { HighlighterIcon } from "@phosphor-icons/react/Highlighter";
import { NotePencilIcon } from "@phosphor-icons/react/NotePencil";
import type { Editor } from "@tiptap/core";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { requestErrorMessage } from "../../api/messages";
import type { Annotation } from "../../api/schema";
import { type Segment, selectionSegments } from "../highlights";
import type { Annotations } from "../useAnnotations";

/*
 * The controls for highlights and notes (docs/adr/0007-annotations-and-anchoring.md),
 * loaded as their own chunk once the editor is up: the toolbar over a
 * selection, the popover of a highlight, and the undo of a removal.
 */

type Props = {
  editor: Editor;
  annotations: Annotations;
  /** The element the controls are placed in, around the reader. */
  frame: HTMLElement;
};

export function AnnotationsUI({ editor, annotations, frame }: Props) {
  const [open, setOpen] = useState<{ id: string; noteFirst: boolean }>();
  const [removed, setRemoved] = useState<Annotation>();
  const [error, setError] = useState<string>();

  const run = async (action: () => Promise<unknown>) => {
    setError(undefined);
    try {
      await action();
    } catch (e) {
      setError(requestErrorMessage(e));
    }
  };

  const highlight = (segments: readonly Segment[], withNote: boolean) =>
    run(async () => {
      const created = await annotations.create(segments);
      editor.commands.setTextSelection(editor.state.selection.to);
      const first = created[0];
      if (withNote && first) setOpen({ id: first.id, noteFirst: true });
      else editor.commands.focus();
    });

  // A click on a highlight opens its popover.
  useEffect(() => {
    const dom = editor.view.dom;
    const onClick = (event: MouseEvent) => {
      const mark = (event.target as Element).closest?.("[data-annotation]");
      const id = mark?.getAttribute("data-annotation");
      if (id) setOpen({ id, noteFirst: false });
    };
    dom.addEventListener("click", onClick);
    return () => dom.removeEventListener("click", onClick);
  }, [editor]);

  const current = open && annotations.list?.find((a) => a.id === open.id);
  return (
    <>
      <SelectionToolbar editor={editor} frame={frame} onHighlight={highlight} />
      {current && (
        <HighlightPopover
          key={current.id}
          annotation={current}
          editor={editor}
          frame={frame}
          focusNote={open.noteFirst}
          onNote={(note) => run(() => annotations.setNote(current, note))}
          onRemove={() =>
            run(async () => {
              await annotations.remove(current);
              setOpen(undefined);
              setRemoved(current);
              editor.commands.focus();
            })
          }
          onClose={() => {
            setOpen(undefined);
            editor.commands.focus();
          }}
        />
      )}
      {removed && (
        <UndoRemove
          key={removed.id}
          onUndo={() =>
            run(async () => {
              setRemoved(undefined);
              await annotations.restore(removed);
            })
          }
          onDone={() => setRemoved(undefined)}
        />
      )}
      {error && (
        <p
          role="alert"
          className="fixed bottom-4 left-1/2 z-30 -translate-x-1/2 rounded-md border border-rule bg-paper px-3 py-2 text-ui-lg"
        >
          {error}
        </p>
      )}
    </>
  );
}

/** Where to put a box at the viewport coordinates x and y, inside frame. */
function inFrame(frame: HTMLElement, x: number, y: number, width: number) {
  const box = frame.getBoundingClientRect();
  return {
    top: y - box.top,
    left: Math.max(0, Math.min(x - box.left, box.width - width)),
  };
}

const toolbarWidth = 240;

/**
 * Highlight and Highlight + note, over a selection of text. Mod+Shift+H
 * highlights without it; Alt+F10 moves into it, arrows move between its
 * buttons, and Escape goes back to the text with the selection kept.
 */
function SelectionToolbar({
  editor,
  frame,
  onHighlight,
}: {
  editor: Editor;
  frame: HTMLElement;
  onHighlight: (segments: readonly Segment[], withNote: boolean) => void;
}) {
  // From the editor as it is: the selection may predate this chunk loading.
  const [segments, setSegments] = useState(() => selectionSegments(editor.state));
  // Hidden once focus goes somewhere else on the page. Focus events alone
  // can't show it: a browser window without focus sends none.
  const [away, setAway] = useState(false);
  const toolbar = useRef<HTMLDivElement>(null);
  // Read by the shortcut handler, which is set up once.
  const latest = useRef(onHighlight);
  latest.current = onHighlight;

  useEffect(() => {
    const update = () => {
      setSegments(selectionSegments(editor.state));
      setAway(false);
    };
    const blur = ({ event }: { event: FocusEvent }) => {
      const to = event.relatedTarget as Node | null;
      if (to && !toolbar.current?.contains(to)) setAway(true);
    };
    editor.on("selectionUpdate", update);
    editor.on("blur", blur);
    // The editor's own selection: a key can come before React has drawn
    // the last change.
    editor.storage.highlights.onShortcut = (shortcut) => {
      const segments = selectionSegments(editor.state);
      if (segments.length === 0) return false;
      if (shortcut === "highlight") latest.current(segments, false);
      else toolbar.current?.querySelector("button")?.focus();
      return true;
    };
    return () => {
      editor.off("selectionUpdate", update);
      editor.off("blur", blur);
      editor.storage.highlights.onShortcut = undefined;
    };
  }, [editor]);

  if (segments.length === 0 || away) return null;
  const coords = editor.view.coordsAtPos(editor.state.selection.from);
  const above = coords.top - 40;
  const at = inFrame(frame, coords.left, above < 0 ? coords.bottom + 4 : above, toolbarWidth);

  const move = (event: KeyboardEvent) => {
    const buttons = [...(toolbar.current?.querySelectorAll("button") ?? [])];
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (step) {
      event.preventDefault();
      buttons[(index + step + buttons.length) % buttons.length]?.focus();
    } else if (event.key === "Escape") {
      event.preventDefault();
      editor.commands.focus();
    }
  };

  const one = segments.length === 1;
  return (
    <div
      ref={toolbar}
      role="toolbar"
      aria-label="Highlight"
      aria-orientation="horizontal"
      onKeyDown={move}
      onBlur={(event) => {
        const to = event.relatedTarget;
        if (to && !event.currentTarget.contains(to) && !editor.view.dom.contains(to)) {
          setAway(true);
        }
      }}
      onMouseDown={(event) => event.preventDefault()}
      style={at}
      className="absolute z-20 flex gap-1 rounded-md border border-rule bg-paper p-1 font-sans text-ink text-ui"
    >
      <button
        type="button"
        onClick={() => onHighlight(segments, false)}
        className="flex items-center gap-1.5 rounded-sm px-2 py-1 hover:bg-paper-2 focus-visible:bg-paper-2"
      >
        <HighlighterIcon size={16} aria-hidden />
        Highlight
      </button>
      {one && (
        <button
          type="button"
          tabIndex={-1}
          onClick={() => onHighlight(segments, true)}
          className="flex items-center gap-1.5 rounded-sm px-2 py-1 hover:bg-paper-2 focus-visible:bg-paper-2"
        >
          <NotePencilIcon size={16} aria-hidden />
          Highlight + note
        </button>
      )}
    </div>
  );
}

const popoverWidth = 320;

/**
 * A highlight's note and Remove, under the highlight. The note saves when
 * it loses focus, or on Enter (Shift+Enter for a new line); Escape closes.
 */
function HighlightPopover({
  annotation,
  editor,
  frame,
  focusNote,
  onNote,
  onRemove,
  onClose,
}: {
  annotation: Annotation;
  editor: Editor;
  frame: HTMLElement;
  focusNote: boolean;
  onNote: (note: string) => Promise<void>;
  onRemove: () => void;
  onClose: () => void;
}) {
  const [note, setNote] = useState(annotation.note ?? "");
  const popover = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  // The highlight is drawn by the editor, possibly a transaction after this
  // opens, as for one just created.
  const [mark, setMark] = useState<Element | null>(null);
  useEffect(() => {
    const find = () =>
      setMark(editor.view.dom.querySelector(`[data-annotation="${annotation.id}"]`));
    find();
    editor.on("transaction", find);
    return () => {
      editor.off("transaction", find);
    };
  }, [editor, annotation.id]);

  const shown = mark !== null;
  useEffect(() => {
    if (!shown) return;
    if (focusNote) field.current?.focus();
    else popover.current?.querySelector<HTMLElement>("textarea, button")?.focus();
  }, [focusNote, shown]);

  // A press outside the popover and its highlight closes it.
  useEffect(() => {
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element;
      if (!popover.current?.contains(target) && !target.closest?.("[data-annotation]")) onClose();
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [onClose]);

  if (!mark) return null;
  const rect = mark.getBoundingClientRect();
  const at = inFrame(frame, rect.left, rect.bottom + 4, popoverWidth);

  const save = async () => {
    if (note !== (annotation.note ?? "")) await onNote(note);
  };

  return (
    <div
      ref={popover}
      role="dialog"
      aria-label="Highlight"
      style={{ ...at, width: `min(${popoverWidth}px, 100%)` }}
      className="absolute z-20 space-y-2 rounded-md border border-rule bg-paper p-3 font-sans text-ink text-ui"
    >
      <label className="block space-y-1">
        <span className="text-ink-2">Note</span>
        <textarea
          ref={field}
          value={note}
          maxLength={5000}
          rows={3}
          onChange={(event) => setNote(event.target.value)}
          onBlur={() => void save()}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              void save().then(onClose);
            } else if (event.key === "Escape") {
              event.preventDefault();
              onClose();
            }
          }}
          className="block w-full resize-y rounded-sm border border-rule bg-paper px-2 py-1 text-ui-lg"
        />
      </label>
      <button
        type="button"
        onClick={onRemove}
        onKeyDown={(event) => {
          if (event.key === "Escape") onClose();
        }}
        className="rounded-sm px-2 py-1 text-ink-2 underline underline-offset-2 hover:bg-paper-2 hover:text-ink"
      >
        Remove highlight
      </button>
    </div>
  );
}

/** How long a removal can be undone. */
const undoMs = 5000;

function UndoRemove({ onUndo, onDone }: { onUndo: () => void; onDone: () => void }) {
  // The timer starts once, however often the page re-renders meanwhile.
  const done = useRef(onDone);
  done.current = onDone;
  useEffect(() => {
    const timer = setTimeout(() => done.current(), undoMs);
    return () => clearTimeout(timer);
  }, []);
  return (
    <p
      role="status"
      className="fixed bottom-4 left-1/2 z-30 flex -translate-x-1/2 items-center gap-2 rounded-md border border-rule bg-paper px-3 py-2 font-sans text-ui-lg"
    >
      Highlight removed ·
      <button type="button" onClick={onUndo} className="text-accent underline underline-offset-2">
        Undo
      </button>
    </p>
  );
}

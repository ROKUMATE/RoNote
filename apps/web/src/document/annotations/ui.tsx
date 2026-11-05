import { HighlighterIcon } from "@phosphor-icons/react/Highlighter";
import { NotePencilIcon } from "@phosphor-icons/react/NotePencil";
import type { Editor } from "@tiptap/core";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { requestErrorMessage } from "../../api/messages";
import type { Annotation } from "../../api/schema";
import { selectorOf } from "../anchoring";
import { type Segment, selectionSegments } from "../highlights";
import type { Annotations } from "../useAnnotations";
import { Notes } from "./notes";

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

  const highlight = (segments: readonly Segment[], withNote: boolean, inCode: boolean) =>
    run(async () => {
      const created = await annotations.create(segments);
      const first = created[0];
      if (inCode) {
        // The selection was the browser's, inside the code; focus stays there.
        getSelection()?.collapseToEnd();
      } else {
        editor.commands.setTextSelection(editor.state.selection.to);
      }
      if (withNote && first) setOpen({ id: first.id, noteFirst: true });
      else if (!inCode) editor.commands.focus();
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
  const noted = annotations.list?.filter((a) => a.note) ?? [];
  return (
    <>
      <SelectionToolbar editor={editor} frame={frame} onHighlight={highlight} />
      <Notes
        editor={editor}
        notes={noted}
        frame={frame}
        onOpen={(id) => setOpen({ id, noteFirst: true })}
      />
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

/** Mod+Shift+H and Alt+F10, as the editor's keymap reads them. */
function shortcutOf(event: globalThis.KeyboardEvent): "highlight" | "toolbar" | undefined {
  const mod = /Mac|iP(hone|[oa]d)/.test(navigator.platform) ? event.metaKey : event.ctrlKey;
  if (mod && event.shiftKey && event.key.toLowerCase() === "h") return "highlight";
  if (event.altKey && event.key === "F10") return "toolbar";
  return undefined;
}

/**
 * The browser's selection when it lies inside the text of one code block
 * under root: the block's segment, and where the selection is on screen.
 */
function codeSelection(root: HTMLElement): { segment: Segment; rect: DOMRect } | undefined {
  const selection = getSelection();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return undefined;
  const range = selection.getRangeAt(0);
  const element = (node: Node) => (node instanceof Element ? node : node.parentElement);
  const code = element(range.startContainer)?.closest("pre > code");
  const block = code?.closest<HTMLElement>("[data-block-id]");
  const blockId = block?.dataset.blockId;
  if (!code || !blockId || !root.contains(code) || !code.contains(range.endContainer)) {
    return undefined;
  }
  // The code element's text is the block's: count up to the selection.
  const before = document.createRange();
  before.setStart(code, 0);
  before.setEnd(range.startContainer, range.startOffset);
  const start = before.toString().length;
  const end = start + range.toString().length;
  return {
    segment: { blockId, selector: selectorOf(code.textContent ?? "", start, end) },
    rect: range.getBoundingClientRect(),
  };
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
  onHighlight: (segments: readonly Segment[], withNote: boolean, inCode: boolean) => void;
}) {
  // From the page as it is: the selection may predate this chunk loading.
  const [segments, setSegments] = useState(() => selectionSegments(editor.state));
  const [code, setCode] = useState(() => codeSelection(editor.view.dom));
  // Hidden once focus goes somewhere else on the page. Focus events alone
  // can't show it: a browser window without focus sends none.
  const [away, setAway] = useState(false);
  const toolbar = useRef<HTMLDivElement>(null);
  // Read by the key handlers, which are set up once.
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
    const selectionChange = () => setCode(codeSelection(editor.view.dom));
    // The editor's own selection: a key can come before React has drawn
    // the last change.
    const shortcut = (which: "highlight" | "toolbar", segments: Segment[], inCode: boolean) => {
      if (segments.length === 0) return false;
      if (which === "highlight") latest.current(segments, false, inCode);
      else toolbar.current?.querySelector("button")?.focus();
      return true;
    };
    // Keys inside code never reach the editor's keymap, so they are read here.
    const codeKeys = (event: globalThis.KeyboardEvent) => {
      const found = codeSelection(editor.view.dom);
      const which = shortcutOf(event);
      if (found && which && shortcut(which, [found.segment], true)) event.preventDefault();
    };
    editor.on("selectionUpdate", update);
    editor.on("blur", blur);
    document.addEventListener("selectionchange", selectionChange);
    editor.view.dom.addEventListener("keydown", codeKeys);
    editor.storage.highlights.onShortcut = (which) =>
      shortcut(which, selectionSegments(editor.state), false);
    // A selection made between the first render and now sent its events
    // before anyone listened.
    setSegments(selectionSegments(editor.state));
    selectionChange();
    return () => {
      editor.off("selectionUpdate", update);
      editor.off("blur", blur);
      document.removeEventListener("selectionchange", selectionChange);
      editor.view.dom.removeEventListener("keydown", codeKeys);
      editor.storage.highlights.onShortcut = undefined;
    };
  }, [editor]);

  let shown: { segments: Segment[]; x: number; top: number; bottom: number; inCode: boolean };
  if (code) {
    const { left, top, bottom } = code.rect;
    shown = { segments: [code.segment], x: left, top, bottom, inCode: true };
  } else if (segments.length > 0 && !away) {
    const { left, top, bottom } = editor.view.coordsAtPos(editor.state.selection.from);
    shown = { segments, x: left, top, bottom, inCode: false };
  } else {
    return null;
  }
  const above = shown.top - 40;
  const at = inFrame(frame, shown.x, above < 0 ? shown.bottom + 4 : above, toolbarWidth);

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

  const one = shown.segments.length === 1;
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
        onClick={() => onHighlight(shown.segments, false, shown.inCode)}
        className="flex items-center gap-1.5 rounded-sm px-2 py-1 hover:bg-paper-2 focus-visible:bg-paper-2"
      >
        <HighlighterIcon size={16} aria-hidden />
        Highlight
      </button>
      {one && (
        <button
          type="button"
          tabIndex={-1}
          onClick={() => onHighlight(shown.segments, true, shown.inCode)}
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

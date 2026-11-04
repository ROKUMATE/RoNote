import { PlusIcon } from "@phosphor-icons/react/Plus";
import type { Editor } from "@tiptap/core";
import type { EditorView } from "@tiptap/pm/view";
import { EditorContent, useEditor } from "@tiptap/react";
import { type MouseEvent, useEffect, useRef, useState } from "react";
import type { DocBlock, DocContent } from "../api/schema";
import { BlockMenu } from "./BlockMenu";
import { ConfirmRevert } from "./ConfirmRevert";
import { BlockIdentityPlugin, revertMeta } from "./identity";
import { type InsertAt, insertUserBlock, SlashMenu, topLevelBlockAt } from "./insert";
import { toEditor, topLevelNode } from "./mapping";
import { readerExtensions } from "./views";

/**
 * A link in the text: an anchor scrolls to its heading in this document and
 * moves focus there; any other link opens in a new tab. Same-domain import
 * on click comes later (P11).
 */
function followLink(_view: EditorView, event: globalThis.MouseEvent): boolean {
  const link = (event.target as Element | null)?.closest?.("a[href]");
  if (!link) return false;
  event.preventDefault();
  const href = link.getAttribute("href") ?? "";
  if (href.startsWith("#")) {
    const target = document.getElementById(decodeURIComponent(href.slice(1)));
    if (target) {
      target.tabIndex = -1;
      target.scrollIntoView();
      target.focus({ preventScroll: true });
    }
    return true;
  }
  window.open(href, "_blank", "noopener,noreferrer");
  return true;
}

/**
 * The document's body, read and edited in place. A "+" below the block under
 * the pointer or the cursor, or "/" in an empty paragraph, opens the menu of
 * blocks a user can add.
 */
export function Reader({
  content,
  onEditor,
  onRevert,
}: {
  content: DocContent;
  onEditor?: (editor: Editor) => void;
  /** Puts a source block back on the server, and returns it as stored. */
  onRevert?: (blockId: string) => Promise<DocBlock>;
}) {
  const wrapper = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ at: InsertAt; top: number }>();
  const [target, setTarget] = useState<{ pos: number; top: number }>();
  const open = useRef<(at: InsertAt) => void>(() => {});
  const [reverting, setReverting] = useState<string>();

  // Built once: useEditor reapplies options that change between renders,
  // which would redraw the document and lose the caret mid-typing.
  const [options] = useState(() => ({
    extensions: [
      ...readerExtensions,
      SlashMenu.configure({ open: (at: InsertAt) => open.current(at) }),
      BlockIdentityPlugin.configure({ onRevert: setReverting }),
    ],
    content: toEditor(content),
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    editorProps: {
      attributes: { "aria-label": "Document text", class: "reader-content" },
      handleDOMEvents: { click: followLink },
    },
  }));
  const editor = useEditor(options);

  /** The bottom of the top-level block at pos, from the top of the reader. */
  const bottomOf = (pos: number) => {
    const block = editor.view.nodeDOM(pos);
    const top = wrapper.current?.getBoundingClientRect().top ?? 0;
    return block instanceof HTMLElement ? block.getBoundingClientRect().bottom - top : 0;
  };
  open.current = (at) => setMenu({ at, top: bottomOf("replace" in at ? at.replace : at.after) });

  const reportEditor = useRef(onEditor);
  useEffect(() => {
    reportEditor.current?.(editor);
  }, [editor]);

  useEffect(() => {
    const follow = () => {
      const pos = topLevelBlockAt(editor, editor.state.selection.from);
      setTarget(pos === undefined ? undefined : { pos, top: bottomOf(pos) });
    };
    const forget = () => setTarget(undefined);
    editor.on("selectionUpdate", follow);
    editor.on("update", forget);
    return () => {
      editor.off("selectionUpdate", follow);
      editor.off("update", forget);
    };
  });

  const hover = (event: MouseEvent) => {
    const hit = editor.view.posAtCoords({ left: event.clientX, top: event.clientY });
    const pos = hit ? topLevelBlockAt(editor, hit.inside >= 0 ? hit.inside : hit.pos) : undefined;
    if (pos !== undefined && pos !== target?.pos) setTarget({ pos, top: bottomOf(pos) });
  };

  const revert = async (blockId: string) => {
    setReverting(undefined);
    if (!onRevert) return;
    replaceBlock(editor, await onRevert(blockId));
  };

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: hovering only places the "+" button; the cursor and "/" do the same from the keyboard.
    <div ref={wrapper} className="relative" onMouseMove={hover}>
      <EditorContent editor={editor} className="reader" />
      {target && !menu && (
        <button
          type="button"
          aria-label="Add a block below"
          onClick={() => open.current({ after: target.pos })}
          style={{ top: target.top - 12 }}
          className="absolute -left-8 rounded-sm p-1 text-ink-2 hover:bg-paper-2 hover:text-ink max-sidebar:right-0 max-sidebar:left-auto"
        >
          <PlusIcon size={16} aria-hidden />
        </button>
      )}
      {reverting && (
        <ConfirmRevert
          onConfirm={() => void revert(reverting)}
          onCancel={() => setReverting(undefined)}
        />
      )}
      {menu && (
        <BlockMenu
          top={menu.top}
          onChoose={(kind) => {
            setMenu(undefined);
            insertUserBlock(editor, kind, menu.at);
          }}
          onClose={() => {
            setMenu(undefined);
            editor.commands.focus();
          }}
        />
      )}
    </div>
  );
}

/** Swaps the top-level block with block's id for block, as a revert, not an edit. */
function replaceBlock(editor: Editor, block: DocBlock) {
  const { state } = editor;
  let at: number | undefined;
  state.doc.forEach((node, pos) => {
    if (node.attrs.blockId === block.id) at = pos;
  });
  const old = at === undefined ? undefined : state.doc.nodeAt(at);
  if (at === undefined || !old) return;
  const tr = state.tr.replaceWith(
    at,
    at + old.nodeSize,
    state.schema.nodeFromJSON(topLevelNode(block)),
  );
  editor.view.dispatch(tr.setMeta(revertMeta, true));
}

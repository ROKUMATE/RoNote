import { PlusIcon } from "@phosphor-icons/react/Plus";
import type { Editor } from "@tiptap/core";
import { EditorContent, useEditor } from "@tiptap/react";
import { type MouseEvent, type ReactNode, useEffect, useRef, useState } from "react";
import type { DocBlock, DocContent } from "../api/schema";
import { BlockMenu } from "./BlockMenu";
import { ConfirmRevert } from "./ConfirmRevert";
import { marksByBlock, sameMarks } from "./codeMarks";
import { linkAction, openOriginal } from "./follow";
import { Highlights, placements } from "./highlights";
import { BlockIdentityPlugin, revertMeta } from "./identity";
import { type InsertAt, insertUserBlock, SlashMenu, topLevelBlockAt } from "./insert";
import { LinkStatus, setLinkTargets } from "./linkStatus";
import { followHelpId, type LinkTargets } from "./links";
import { toEditor, topLevelNode } from "./mapping";
import { moveTo } from "./moveTo";
import { CodeMarksContext, LinkTargetsContext } from "./render";
import { readerExtensions } from "./views";

/** A same-domain link being followed, and how far that has got. */
export type Following = {
  link: HTMLElement;
  /** The step an import is at, or the sentence saying why it failed. */
  text: string;
  failed: boolean;
  href: string;
};

/**
 * The document's body, read and edited in place. A "+" below the block under
 * the pointer or the cursor, or "/" in an empty paragraph, opens the menu of
 * blocks a user can add. Links are followed, not edited, when clicked: their
 * text stays editable from the keyboard.
 */
export function Reader({
  content,
  onEditor,
  onRevert,
  linkTargets,
  onFollow,
  following,
}: {
  content: DocContent;
  onEditor?: (editor: Editor) => void;
  /** Puts a source block back on the server, and returns it as stored. */
  onRevert?: (blockId: string) => Promise<DocBlock>;
  /** Where the document's same-domain links lead, once resolved. */
  linkTargets?: LinkTargets | undefined;
  /** Follows a same-domain link: opens or imports its page. */
  onFollow?: (href: string, link: HTMLElement) => void;
  following?: Following | undefined;
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
      LinkStatus,
      Highlights,
    ],
    content: toEditor(content),
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    editorProps: {
      attributes: { "aria-label": "Document text", class: "reader-content" },
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

  useEffect(() => {
    if (linkTargets) setLinkTargets(editor.view, linkTargets);
  }, [editor, linkTargets]);

  // Code views draw their own highlights; they redraw only when theirs move.
  const [codeMarks, setCodeMarks] = useState(() => marksByBlock(placements(editor.state)));
  useEffect(() => {
    let last = placements(editor.state);
    const update = () => {
      const current = placements(editor.state);
      if (current === last) return;
      last = current;
      const next = marksByBlock(current);
      setCodeMarks((old) => (sameMarks(old, next) ? old : next));
    };
    editor.on("transaction", update);
    return () => {
      editor.off("transaction", update);
    };
  }, [editor]);

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

  /** The link a mouse event happened on, if it is one in this document. */
  const linkOf = (event: MouseEvent) =>
    (event.target as Element).closest?.<HTMLAnchorElement>(".reader a[href]") ?? undefined;

  // A plain press on a link must not reach the editor, which would put the
  // caret in it: clicking a link follows it.
  const pressLink = (event: MouseEvent) => {
    if (
      event.button === 0 &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.shiftKey &&
      linkOf(event)
    ) {
      event.preventDefault();
      event.stopPropagation();
    }
  };

  const clickLink = (event: MouseEvent) => {
    const link = linkOf(event);
    const action = link && linkAction(link, event.nativeEvent);
    if (!link || !action) return;
    event.preventDefault();
    if (action.do === "open") {
      openOriginal(action.href);
    } else if (action.do === "follow") {
      onFollow?.(action.href, link);
    } else {
      const heading = document.getElementById(decodeURIComponent(action.fragment));
      if (heading && editor.view.dom.contains(heading)) moveTo(editor.view, heading);
    }
  };

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: hovering only places the "+" button, and the click handlers serve the links inside, which are focusable themselves.
    // biome-ignore lint/a11y/useKeyWithClickEvents: links are followed from the keyboard by the browser's own Enter on a focused link.
    <div
      ref={wrapper}
      className="relative"
      onMouseMove={hover}
      onMouseDownCapture={pressLink}
      onClick={clickLink}
      onAuxClick={clickLink}
    >
      <LinkTargetsContext value={linkTargets}>
        <CodeMarksContext value={codeMarks}>
          <EditorContent editor={editor} className="reader" />
        </CodeMarksContext>
      </LinkTargetsContext>
      <p id={followHelpId} hidden>
        Opens this page in RoNote, importing it first if it is not in your library.
      </p>
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
      {following && <FollowProgress following={following} wrapper={wrapper.current} />}
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

/**
 * How following a link is going, just below the link: the import's step,
 * or why it failed, with a way to the original page.
 */
function FollowProgress({
  following,
  wrapper,
}: {
  following: Following;
  wrapper: HTMLElement | null;
}) {
  const box = wrapper?.getBoundingClientRect();
  const at = following.link.getBoundingClientRect();
  const style = {
    top: at.bottom - (box?.top ?? 0) + 4,
    left: Math.max(0, at.left - (box?.left ?? 0)),
  };
  let body: ReactNode = `${following.text}…`;
  if (following.failed) {
    body = (
      <>
        {following.text}{" "}
        <a
          href={following.href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-accent underline"
        >
          Open original
        </a>
      </>
    );
  }
  return (
    <p
      role={following.failed ? "alert" : "status"}
      style={style}
      className="absolute z-10 max-w-[min(28rem,100%)] rounded-sm border border-rule bg-paper px-2 py-1 font-sans text-ink text-ui"
    >
      {body}
    </p>
  );
}

import { ArrowSquareOutIcon } from "@phosphor-icons/react/ArrowSquareOut";
import type { Editor } from "@tiptap/core";
import { useEffect, useState } from "react";
import { apiBase } from "../api/client";

/** Where the link sits: above its block, at the frame's right. */
interface Placement {
  page: number;
  top: number;
}

/**
 * "View page N" for the block of a PDF's document under the pointer, or
 * holding the caret: it opens the original PDF at the block's page in a new
 * tab (docs/adr/0013-pdf-import.md). The browser shows the PDF sandboxed.
 */
export function PageLink({
  editor,
  frame,
  documentId,
}: {
  editor: Editor;
  frame: HTMLElement;
  documentId: string;
}) {
  const [at, setAt] = useState<Placement>();

  useEffect(() => {
    const view = editor.view;
    const root = view.dom;
    // The top-level block an element is in, and where it starts.
    const place = (el: Element | null) => {
      let block = el;
      while (block && block.parentElement !== root) block = block.parentElement;
      if (!block) return;
      // Its node, by where the editor maps it: a node view's top element
      // carries no block id.
      const { doc } = view.state;
      const index = doc.resolve(view.posAtDOM(block, 0)).index(0);
      const page: number | undefined =
        index < doc.childCount ? (doc.child(index).attrs.page ?? undefined) : undefined;
      if (!page) return;
      const top = block.getBoundingClientRect().top - frame.getBoundingClientRect().top;
      const found = page;
      setAt((was) => (was && was.page === found && was.top === top ? was : { page: found, top }));
    };
    const pointer = (event: MouseEvent) => place(event.target as Element);
    const caret = () => {
      const { $from } = view.state.selection;
      if ($from.depth < 1) return;
      const dom = view.nodeDOM($from.before(1));
      if (dom instanceof Element) place(dom);
    };
    root.addEventListener("mouseover", pointer);
    editor.on("selectionUpdate", caret);
    editor.on("focus", caret);
    return () => {
      root.removeEventListener("mouseover", pointer);
      editor.off("selectionUpdate", caret);
      editor.off("focus", caret);
    };
  }, [editor, frame]);

  if (!at) return null;
  return (
    <a
      href={`${apiBase}/documents/${documentId}/original#page=${at.page}`}
      target="_blank"
      rel="noopener noreferrer"
      style={{ top: at.top }}
      className="absolute right-0 z-10 inline-flex -translate-y-full items-center gap-1 rounded-sm bg-paper px-1 text-ink-2 text-ui hover:text-ink hover:underline"
    >
      View page {at.page}
      <ArrowSquareOutIcon size={16} aria-hidden />
      <span className="sr-only">(opens in a new tab)</span>
    </a>
  );
}

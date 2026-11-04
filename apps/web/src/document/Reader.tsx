import type { EditorView } from "@tiptap/pm/view";
import { EditorContent, useEditor } from "@tiptap/react";
import type { DocContent } from "../api/schema";
import { toEditor } from "./mapping";
import { readerExtensions } from "./views";

/**
 * A link in the text: an anchor scrolls to its heading in this document and
 * moves focus there; any other link opens in a new tab. Same-domain import
 * on click comes later (P11).
 */
function followLink(_view: EditorView, event: MouseEvent): boolean {
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

/** The document's body, read and edited in place. */
export function Reader({ content }: { content: DocContent }) {
  const editor = useEditor({
    extensions: readerExtensions,
    content: toEditor(content),
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    editorProps: {
      attributes: { "aria-label": "Document text", class: "reader-content" },
      handleDOMEvents: { click: followLink },
    },
  });
  return <EditorContent editor={editor} className="reader" />;
}

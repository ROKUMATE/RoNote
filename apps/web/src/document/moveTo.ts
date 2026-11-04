import { TextSelection } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";

/**
 * Scrolls to a block of the document and moves focus there. Focus in an
 * editor is its caret, so the caret goes to the start of the block.
 */
export function moveTo(view: EditorView, block: HTMLElement) {
  block.scrollIntoView();
  const pos = view.posAtDOM(block, 0);
  view.dispatch(view.state.tr.setSelection(TextSelection.near(view.state.doc.resolve(pos))));
  view.focus();
}

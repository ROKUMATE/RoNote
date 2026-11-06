/*
 * Mod+Enter (Cmd on a Mac, Ctrl elsewhere) runs the focused code block: the
 * one holding focus, as its code or a button does, or the one the editor
 * has selected. One listener serves every block that has a Run.
 */

/** Whether Cmd, rather than Ctrl, is the modifier, as on a Mac. */
const usesCmd = /Mac|iPhone|iPad/.test(navigator.platform);

/** The shortcut as aria-keyshortcuts names it, and as a reader sees it. */
export const runShortcut = usesCmd ? "Meta+Enter" : "Control+Enter";
export const runShortcutLabel = usesCmd ? "⌘Enter" : "Ctrl+Enter";

/** Each block's Run, by block id, while it has one. */
const actions = new Map<string, () => void>();

/** The code block the reader is in: focus inside it, or the editor's selected block. */
function focusedBlock(): string | undefined {
  const active = document.activeElement;
  const inside = active?.closest<HTMLElement>(".code-block[data-block-id]");
  if (inside) return inside.dataset.blockId;
  const selected = active?.closest(".ProseMirror")?.querySelector(".ProseMirror-selectednode");
  const blocks = selected?.querySelectorAll<HTMLElement>(".code-block[data-block-id]") ?? [];
  // A selected block holding several, such as tabs, names none of them.
  return blocks.length === 1 ? blocks[0]?.dataset.blockId : undefined;
}

function pressed(event: KeyboardEvent) {
  if (event.key !== "Enter" || event.shiftKey || event.altKey) return;
  if (usesCmd ? !event.metaKey || event.ctrlKey : !event.ctrlKey || event.metaKey) return;
  const blockId = focusedBlock();
  const run = blockId === undefined ? undefined : actions.get(blockId);
  if (!run) return;
  // Before the editor, which would otherwise take the key.
  event.preventDefault();
  event.stopPropagation();
  run();
}

/** Lets Mod+Enter run blockId with run; returns what takes it back. */
export function onRunKey(blockId: string, run: () => void): () => void {
  if (actions.size === 0) window.addEventListener("keydown", pressed, true);
  actions.set(blockId, run);
  return () => {
    if (actions.get(blockId) === run) actions.delete(blockId);
    if (actions.size === 0) window.removeEventListener("keydown", pressed, true);
  };
}

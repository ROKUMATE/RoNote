import { useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import { useEffect, useRef, useState } from "react";
import { documentQuery, revertBlock, saveDocument } from "../api/queries";
import type { DocBlock, Document } from "../api/schema";
import { revertMeta } from "./identity";
import { fromEditor } from "./mapping";
import { Autosave, type SaveStatus } from "./save";

/**
 * Editing one document: autosave of the editor's content, and reverting a
 * block once pending edits are saved. Unedited source blocks are saved as
 * loaded (or as a revert returned them), never re-serialized.
 */
export function useEditing(doc: Document) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<SaveStatus>({ state: "idle" });
  const editor = useRef<Editor | undefined>(undefined);
  const originals = useRef(
    new Map(
      doc.content.blocks.filter((b) => b.origin === "source" && !b.edited).map((b) => [b.id, b]),
    ),
  );
  const [autosave] = useState(
    () =>
      new Autosave({
        version: doc.version,
        save: (content, version, options) => saveDocument(doc.id, content, version, options),
        read: () => fromEditor(editor.current?.getJSON() ?? { type: "doc" }, originals.current),
        onStatus: setStatus,
      }),
  );

  useEffect(() => {
    const onHidden = () => {
      if (document.visibilityState === "hidden") void autosave.flush({ keepalive: true });
    };
    document.addEventListener("visibilitychange", onHidden);
    return () => {
      document.removeEventListener("visibilitychange", onHidden);
      autosave.dispose();
    };
  }, [autosave]);

  const onEditor = (e: Editor) => {
    editor.current = e;
    e.on("update", ({ transaction }) => {
      if (!transaction.getMeta(revertMeta)) autosave.change();
    });
    e.on("blur", () => void autosave.flush());
  };

  const revert = async (blockId: string): Promise<DocBlock> => {
    await autosave.flush();
    const reverted = await revertBlock(doc.id, blockId);
    autosave.setVersion(reverted.version);
    queryClient.setQueryData(documentQuery(doc.id).queryKey, reverted);
    const block = reverted.content.blocks.find((b) => b.id === blockId);
    if (!block) throw new Error(`block ${blockId} is not in the reverted document`);
    originals.current.set(block.id, block);
    return block;
  };

  return { status, onEditor, revert };
}

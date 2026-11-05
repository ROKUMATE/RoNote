import { useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import { useEffect, useMemo, useRef, useState } from "react";
import { documentQuery, revertBlock, saveDocument } from "../api/queries";
import type { CodeLang, DocBlock, DocContent, Document } from "../api/schema";
import { type ChosenLangs, type CodeLangChoices, unlabelled, withCodeLangs } from "./codeLangs";
import { revertMeta } from "./identity";
import { fromEditor } from "./mapping";
import { Autosave, type SaveStatus } from "./save";
import type { SavedContent } from "./useAnnotations";

/**
 * Editing one document: autosave of the editor's content and the chosen
 * code languages, and reverting a block once pending edits are saved.
 * Unedited source blocks are saved as loaded (or as a revert returned
 * them), never re-serialized.
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
  const [langs, setLangs] = useState<ChosenLangs>(doc.content.code_langs ?? {});
  const [savedListeners] = useState(() => new Set<(content: DocContent) => void>());
  const announce = (content: DocContent) => {
    for (const listener of savedListeners) listener(content);
  };
  const chosen = useRef(langs);
  const [autosave] = useState(
    () =>
      new Autosave({
        version: doc.version,
        save: async (content, version, options) => {
          const saved = await saveDocument(doc.id, content, version, options);
          announce(content);
          return saved;
        },
        read: () =>
          withCodeLangs(
            fromEditor(editor.current?.getJSON() ?? { type: "doc" }, originals.current),
            chosen.current,
          ),
        onStatus: setStatus,
      }),
  );
  // Stable, so code blocks re-render when a language changes, not on every save.
  const [choose] = useState(() => {
    const set = (next: ChosenLangs) => {
      chosen.current = next;
      setLangs(next);
      autosave.change();
    };
    return {
      choose: (blockId: string, lang: CodeLang | undefined) => {
        const rest = Object.entries(chosen.current).filter(([id]) => id !== blockId);
        set(Object.fromEntries(lang ? [...rest, [blockId, lang]] : rest));
      },
      chooseForUnlabelled: (lang: CodeLang) => {
        const ids = unlabelled(doc.content.blocks, chosen.current);
        set({ ...chosen.current, ...Object.fromEntries(ids.map((id) => [id, lang])) });
      },
    };
  });
  const codeLangs: CodeLangChoices = useMemo(() => ({ chosen: langs, ...choose }), [langs, choose]);

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
    announce(reverted.content);
    return block;
  };

  // Stable, so effects that hear of saves subscribe once.
  const [saving] = useState<SavedContent>(() => ({
    flush: () => autosave.flush(),
    subscribeSaved: (listener) => {
      savedListeners.add(listener);
      return () => savedListeners.delete(listener);
    },
  }));

  return { status, onEditor, revert, codeLangs, saving };
}

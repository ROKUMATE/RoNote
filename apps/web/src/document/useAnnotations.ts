import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import { useCallback, useEffect, useState } from "react";
import {
  annotationsQuery,
  createAnnotation,
  deleteAnnotation,
  reanchorAnnotations,
  updateAnnotation,
} from "../api/queries";
import type { Annotation, DocContent } from "../api/schema";
import { type Segment, setHighlights } from "./highlights";
import { ReanchorSync } from "./reanchorSync";

/** What the editing hook offers annotations: saving pending edits, and hearing of each save. */
export type SavedContent = {
  flush: () => Promise<void>;
  subscribeSaved: (listener: (content: DocContent) => void) => () => void;
};

/**
 * A document's highlights and notes (docs/adr/0007-annotations-and-anchoring.md):
 * loaded, drawn in the editor, created and changed through the API, and
 * re-anchored after every content save (ReanchorSync).
 */
export function useAnnotations(
  documentId: string,
  initialContent: DocContent,
  editor: Editor | undefined,
  saving: SavedContent,
) {
  const queryClient = useQueryClient();
  const query = annotationsQuery(documentId);
  const list = useQuery(query).data;

  useEffect(() => {
    if (editor && list) setHighlights(editor.view, list);
  }, [editor, list]);

  const update = useCallback(
    (change: (list: Annotation[]) => Annotation[]) =>
      queryClient.setQueryData(query.queryKey, (old) => change(old ?? [])),
    [queryClient, query.queryKey],
  );
  const replace = useCallback(
    (saved: readonly Annotation[]) => {
      const byId = new Map(saved.map((a) => [a.id, a]));
      update((old) => old.map((a) => byId.get(a.id) ?? a));
    },
    [update],
  );

  const [sync] = useState(
    () =>
      new ReanchorSync(initialContent, {
        current: () => queryClient.getQueryData(query.queryKey) ?? [],
        send: (moves) => reanchorAnnotations(documentId, moves),
        saved: replace,
        reload: () => queryClient.invalidateQueries({ queryKey: query.queryKey }),
      }),
  );
  useEffect(() => saving.subscribeSaved((content) => sync.contentSaved(content)), [saving, sync]);
  // Selectors saved against older text (a tab closed before its batch
  // went) move once the list loads.
  const loaded = list !== undefined;
  useEffect(() => {
    if (loaded) sync.check();
    return () => sync.dispose();
  }, [loaded, sync]);

  /** Highlights each segment; pending edits are saved first, so the server has the text. */
  const create = async (segments: readonly Segment[], note = ""): Promise<Annotation[]> => {
    await saving.flush();
    const created = await Promise.all(
      segments.map((s) =>
        createAnnotation(documentId, {
          block_id: s.blockId,
          kind: "highlight",
          selector: s.selector,
          ...(note ? { note } : {}),
        }),
      ),
    );
    update((old) => [...old, ...created]);
    return created;
  };

  const setNote = async (annotation: Annotation, note: string) => {
    replace([await updateAnnotation(annotation.id, { version: annotation.version, note })]);
  };

  const remove = async (annotation: Annotation) => {
    await deleteAnnotation(annotation.id);
    update((old) => old.filter((a) => a.id !== annotation.id));
  };

  /** Puts a removed highlight back, as a new annotation with its range and note. */
  const restore = async (annotation: Annotation) => {
    await saving.flush();
    const restored = await createAnnotation(documentId, {
      block_id: annotation.block_id,
      kind: annotation.kind,
      selector: annotation.selector,
      ...(annotation.note ? { note: annotation.note } : {}),
    });
    update((old) => [...old, restored]);
  };

  return { list, create, setNote, remove, restore };
}

export type Annotations = ReturnType<typeof useAnnotations>;

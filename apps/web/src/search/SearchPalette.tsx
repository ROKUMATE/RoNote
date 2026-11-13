import { MagnifyingGlassIcon } from "@phosphor-icons/react/MagnifyingGlass";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type KeyboardEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { requestErrorMessage } from "../api/messages";
import { searchQuery } from "../api/queries";
import type { SearchResult } from "../api/schema";

/** How long typing must pause before a search goes out. */
const debounceMs = 150;

/** What a search says when it could only match words (ADR 0011). */
export const degradedMessage =
  "Search by meaning is unavailable right now, so these are word matches.";

/** Results, in order, under the document each belongs to. */
type Group = { id: string; title: string; results: { result: SearchResult; index: number }[] };

function groupByDocument(results: SearchResult[]): Group[] {
  const groups = new Map<string, Group>();
  results.forEach((result, index) => {
    let group = groups.get(result.document_id);
    if (!group) {
      group = { id: result.document_id, title: result.document_title, results: [] };
      groups.set(result.document_id, group);
    }
    group.results.push({ result, index });
  });
  return [...groups.values()];
}

/**
 * The search palette (Mod+K): a modal dialog with a search box, results
 * grouped by document, arrow keys to move and Enter to open one at its
 * block. Opened from a document, it can search that document alone.
 */
export function SearchPalette({
  documentId,
  onClose,
}: {
  documentId?: string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const listId = useId();
  const titleId = useId();
  const [typed, setTyped] = useState("");
  const [q, setQ] = useState("");
  const [thisDocument, setThisDocument] = useState(false);
  // The active option belongs to the results it was chosen among: new
  // results start at the top.
  const [pick, setPick] = useState<{ among?: unknown; index: number }>({ index: 0 });
  const navigate = useNavigate();

  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  useEffect(() => {
    const timer = setTimeout(() => setQ(typed.trim()), debounceMs);
    return () => clearTimeout(timer);
  }, [typed]);

  const scope = thisDocument ? documentId : undefined;
  const search = useQuery(searchQuery(q, scope));
  const results = search.data?.results ?? [];
  const groups = useMemo(() => groupByDocument(results), [results]);
  const active = pick.among === search.data ? pick.index : 0;
  const setActive = (index: number) => setPick({ among: search.data, index });
  const current = results[Math.min(active, results.length - 1)];
  const optionId = (index: number) => `${listId}-${index}`;

  useEffect(() => {
    if (current) document.getElementById(optionId(active))?.scrollIntoView({ block: "nearest" });
  });

  const open = (result: SearchResult) => {
    dialog.current?.close();
    void navigate({
      to: "/doc/$id",
      params: { id: result.document_id },
      search: { block: result.block_id },
    });
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (results.length === 0) return;
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((active + step + results.length) % results.length);
    } else if (event.key === "Enter" && current) {
      event.preventDefault();
      open(current);
    }
  };

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      onClose={onClose}
      className="m-auto mt-[10vh] flex max-h-[80vh] w-[min(40rem,calc(100vw-2rem))] flex-col rounded-md border border-rule bg-paper p-0 text-ink backdrop:bg-ink/30 [&:not([open])]:hidden"
    >
      <h2 id={titleId} className="sr-only">
        Search your library
      </h2>
      <div className="flex items-center gap-2 border-rule border-b px-4 py-3">
        <MagnifyingGlassIcon size={16} aria-hidden className="shrink-0 text-ink-2" />
        <input
          type="search"
          role="combobox"
          aria-label="Search your library"
          aria-expanded={results.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          {...(current ? { "aria-activedescendant": optionId(active) } : {})}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={onKeyDown}
          maxLength={500}
          placeholder="Search your library"
          className="min-w-0 flex-1 bg-transparent text-ui-lg outline-none placeholder:text-ink-2"
        />
        {documentId && (
          <label className="flex shrink-0 items-center gap-1.5 text-ink-2 text-ui">
            <input
              type="checkbox"
              checked={thisDocument}
              onChange={(e) => setThisDocument(e.target.checked)}
            />
            This document
          </label>
        )}
      </div>
      <div className="min-h-0 overflow-y-auto px-2 py-2">
        {search.data?.degraded && (
          <p role="status" className="px-2 py-1 text-ink-2 text-ui">
            {degradedMessage}
          </p>
        )}
        {search.isError && (
          <p role="alert" className="px-2 py-1 text-ui-lg">
            {requestErrorMessage(search.error)}
          </p>
        )}
        {q !== "" && search.isSuccess && results.length === 0 && (
          <p role="status" className="px-2 py-1 text-ink-2 text-ui-lg">
            No matches.
          </p>
        )}
        <div id={listId} role="listbox" aria-label="Results">
          {groups.map((group) => (
            // biome-ignore lint/a11y/useSemanticElements: a listbox groups its options with role group; a fieldset is for form controls.
            <div key={group.id} role="group" aria-label={group.title} className="mt-1 first:mt-0">
              <p aria-hidden className="truncate px-2 pt-1 pb-0.5 font-semibold text-ink-2 text-ui">
                {group.title}
              </p>
              {group.results.map(({ result, index }) => (
                // biome-ignore lint/a11y/useKeyWithClickEvents: the combobox's input moves through the options with the arrow keys and opens one with Enter.
                <div
                  key={`${result.block_id}-${index}`}
                  id={optionId(index)}
                  role="option"
                  tabIndex={-1}
                  aria-selected={index === active}
                  onClick={() => open(result)}
                  onMouseMove={() => setActive(index)}
                  className="cursor-pointer rounded-sm px-2 py-1.5 aria-selected:bg-paper-2"
                >
                  {result.heading_path.length > 0 && (
                    <p className="truncate text-ink-2 text-ui">{result.heading_path.join(" › ")}</p>
                  )}
                  <p className="line-clamp-2 text-ui-lg [overflow-wrap:anywhere]">
                    {result.snippet.map((part, i) =>
                      part.mark ? (
                        // biome-ignore lint/suspicious/noArrayIndexKey: parts never move within a snippet.
                        <mark key={i} className="rounded-sm bg-mark text-ink">
                          {part.text}
                        </mark>
                      ) : (
                        // biome-ignore lint/suspicious/noArrayIndexKey: parts never move within a snippet.
                        <span key={i}>{part.text}</span>
                      ),
                    )}
                  </p>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </dialog>
  );
}

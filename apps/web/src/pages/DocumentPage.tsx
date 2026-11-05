import { ArrowSquareOutIcon } from "@phosphor-icons/react/ArrowSquareOut";
import { InfoIcon } from "@phosphor-icons/react/Info";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useRouterState } from "@tanstack/react-router";
import type { Editor } from "@tiptap/core";
import { lazy, Suspense, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import { requestErrorMessages } from "../api/messages";
import { documentQuery, linkTargets } from "../api/queries";
import type { Document, IrWarning } from "../api/schema";
import { CodeLangsContext } from "../document/codeLangs";
import { blockCounts } from "../document/counts";
import { sameDomainHrefs } from "../document/links";
import { Outline } from "../document/Outline";
import { Reader } from "../document/Reader";
import { RunScopeProvider } from "../document/run/RunArea";
import type { SaveStatus } from "../document/save";
import { useAnnotations } from "../document/useAnnotations";
import { useEditing } from "../document/useEditing";
import { useFollow } from "../document/useFollow";
import { useScrollAnchor } from "../document/useScrollAnchor";
import { formatDate, hostOf } from "../format";
import { buttonClass } from "../ui/button";
import { PageTitle } from "../ui/PageTitle";

// The highlight controls load once the editor is up (ADR 0007).
const AnnotationsUI = lazy(() =>
  import("../document/annotations/ui").then((m) => ({ default: m.AnnotationsUI })),
);

/** Warnings that mean page chrome may have come along with the content. */
const genericWarnings: ReadonlySet<IrWarning["code"]> = new Set([
  "adapter_fallback",
  "low_confidence",
]);

/** A document: its header, then its content to read and edit. */
export function DocumentPage() {
  const { id } = useParams({ from: "/doc/$id" });
  const doc = useQuery(documentQuery(id));
  const existing = useRouterState({ select: (s) => s.location.state.notice === "existing" });

  if (doc.isPending) {
    return <Placeholder />;
  }
  if (!doc.data) {
    // A malformed ID is a 400; either way there is nothing at this address.
    const missing = doc.error instanceof ApiError && [400, 404].includes(doc.error.status);
    return (
      <div className="max-w-[68ch]">
        <PageTitle>{missing ? "Not in your library" : "Document"}</PageTitle>
        {missing ? (
          <p className="mt-3 text-ui-lg">
            There's no document here.{" "}
            <Link to="/" className="text-accent underline underline-offset-2">
              Go to the library
            </Link>
          </p>
        ) : (
          <p className="mt-3 flex flex-wrap items-center gap-3 text-ui-lg">
            Couldn't load this document.
            <button type="button" onClick={() => void doc.refetch()} className={buttonClass}>
              Retry
            </button>
          </p>
        )}
      </div>
    );
  }
  return <Header key={doc.data.id} doc={doc.data} existing={existing} />;
}

/**
 * Whether the document opens with its title as a level-1 heading, as most
 * pages do. Then that heading is the page's title, and the header does not
 * repeat it.
 */
function opensWithTitle(doc: Document): boolean {
  const first = doc.content.blocks[0];
  return (
    first?.kind === "heading" &&
    first.level === 1 &&
    (first.content ?? []).map((run) => run.text).join("") === doc.title
  );
}

function Header({ doc, existing }: { doc: Document; existing: boolean }) {
  const fallback = doc.warnings.some((w) => genericWarnings.has(w.code));
  const { status, onEditor, revert, codeLangs, saving } = useEditing(doc);
  const [editor, setEditor] = useState<Editor>();
  const annotations = useAnnotations(doc.id, doc.content, editor, saving);
  const [frame, setFrame] = useState<HTMLDivElement | null>(null);
  const [listSlot, setListSlot] = useState<HTMLDivElement | null>(null);
  const hrefs = useMemo(() => sameDomainHrefs(doc.content.blocks), [doc.content.blocks]);
  const targets = useQuery(linkTargets(doc.id, hrefs)).data;
  const { follow, following } = useFollow(targets);
  useScrollAnchor(editor?.view);
  return (
    <article className="max-w-[68ch]">
      {existing && (
        <p role="status" className="mb-4 text-ink-2 text-ui-lg">
          Already in your library
        </p>
      )}
      <header>
        {!opensWithTitle(doc) && <PageTitle>{doc.title || hostOf(doc.source_url)}</PageTitle>}
        <p className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-ink-2 text-ui-lg">
          <span>
            From{" "}
            <a
              href={doc.source_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-ink underline underline-offset-2"
            >
              {hostOf(doc.source_url)}
              <ArrowSquareOutIcon size={16} aria-hidden />
              <span className="sr-only">(opens in a new tab)</span>
            </a>
          </span>
          <span>
            Imported <time dateTime={doc.created_at}>{formatDate(doc.created_at)}</time>
          </span>
        </p>
        <ul aria-label="Blocks" className="mt-2 flex flex-wrap gap-x-3 text-ink-2 text-ui">
          {blockCounts(doc.content.blocks).map((count) => (
            <li key={count}>{count}</li>
          ))}
        </ul>
        <div ref={setListSlot} />
        <SaveState status={status} />
      </header>
      {fallback && (
        <p role="note" className="mt-6 flex gap-2 border-rule border-l-2 pl-3 text-ui-lg">
          <InfoIcon size={16} aria-hidden className="mt-0.5 shrink-0 text-ink-2" />
          Imported with the generic extractor; some page chrome may remain.
        </p>
      )}
      <div ref={setFrame} className="relative mt-8">
        <CodeLangsContext value={codeLangs}>
          <RunScopeProvider documentId={doc.id} blocks={doc.content.blocks}>
            <Reader
              content={doc.content}
              onEditor={(e) => {
                onEditor(e);
                setEditor(e);
              }}
              onRevert={revert}
              linkTargets={targets}
              onFollow={(href, link) => void follow(href, link)}
              following={following}
            />
          </RunScopeProvider>
        </CodeLangsContext>
        {editor && <Outline editor={editor} />}
        {editor && frame && listSlot && (
          <Suspense fallback={null}>
            <AnnotationsUI
              editor={editor}
              annotations={annotations}
              frame={frame}
              listSlot={listSlot}
            />
          </Suspense>
        )}
      </div>
    </article>
  );
}

/** A still outline of the header while the document loads. */
function Placeholder() {
  return (
    <div className="max-w-[68ch]">
      <p role="status" className="sr-only">
        Loading the document
      </p>
      <div aria-hidden="true" className="space-y-3">
        <div className="h-8 w-3/4 rounded-sm bg-paper-2" />
        <div className="h-3 w-1/2 rounded-sm bg-paper-2" />
        <div className="h-3 w-1/3 rounded-sm bg-paper-2" />
      </div>
    </div>
  );
}

const saveLabels: Record<Exclude<SaveStatus["state"], "conflict" | "refused">, string> = {
  idle: "",
  dirty: "",
  saving: "Saving…",
  saved: "Saved",
  offline: "Offline, retrying",
};

/**
 * Where saving stands. A conflict or a refused save stops autosave; Reload
 * shows the document as the server has it, and nothing is overwritten.
 */
function SaveState({ status }: { status: SaveStatus }) {
  if (status.state === "conflict" || status.state === "refused") {
    const message =
      status.state === "conflict"
        ? requestErrorMessages.version_conflict
        : requestErrorMessages[status.code ?? "invalid_content"];
    return (
      <div role="alert" className="mt-3 flex flex-wrap items-center gap-3 text-ui-lg">
        {message}
        <button type="button" onClick={() => window.location.reload()} className={buttonClass}>
          Reload
        </button>
      </div>
    );
  }
  return (
    <p role="status" className="mt-2 min-h-5 text-ink-2 text-ui">
      {saveLabels[status.state]}
    </p>
  );
}

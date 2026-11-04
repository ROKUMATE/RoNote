import { ArrowSquareOutIcon } from "@phosphor-icons/react/ArrowSquareOut";
import { InfoIcon } from "@phosphor-icons/react/Info";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useRouterState } from "@tanstack/react-router";
import { ApiError } from "../api/client";
import { documentQuery } from "../api/queries";
import type { Document, IrWarning } from "../api/schema";
import { blockCounts } from "../document/counts";
import { formatDate, hostOf } from "../format";
import { buttonClass } from "../ui/button";
import { PageTitle } from "../ui/PageTitle";

/** Warnings that mean page chrome may have come along with the content. */
const genericWarnings: ReadonlySet<IrWarning["code"]> = new Set([
  "adapter_fallback",
  "low_confidence",
]);

/** A document's header. The reader that shows its content comes in P10. */
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
  return <Header doc={doc.data} existing={existing} />;
}

function Header({ doc, existing }: { doc: Document; existing: boolean }) {
  const fallback = doc.warnings.some((w) => genericWarnings.has(w.code));
  return (
    <article className="max-w-[68ch]">
      {existing && (
        <p role="status" className="mb-4 text-ink-2 text-ui-lg">
          Already in your library
        </p>
      )}
      <header>
        <PageTitle>{doc.title || hostOf(doc.source_url)}</PageTitle>
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
      </header>
      {fallback && (
        <p role="note" className="mt-6 flex gap-2 border-rule border-l-2 pl-3 text-ui-lg">
          <InfoIcon size={16} aria-hidden className="mt-0.5 shrink-0 text-ink-2" />
          Imported with the generic extractor; some page chrome may remain.
        </p>
      )}
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

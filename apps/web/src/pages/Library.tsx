import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { documentPages } from "../api/queries";
import type { DocumentSummary } from "../api/schema";
import { formatDate, hostOf } from "../format";
import { endingMessage, stepLabels } from "../imports/describe";
import { ImportButton } from "../imports/ImportDialog";
import type { TrackedImport } from "../imports/state";
import { useImportTracker, useTrackedImports } from "../imports/tracker";
import { buttonClass } from "../ui/button";
import { PageTitle } from "../ui/PageTitle";

const row = "flex items-start justify-between gap-4 border-rule border-b py-3";

export function Library() {
  const pages = useInfiniteQuery(documentPages);
  const pending = useTrackedImports().filter((i) => i.status !== "done");
  const documents = pages.data?.pages.flatMap((page) => page.documents) ?? [];

  return (
    <div className="max-w-[68ch]">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <PageTitle>Library</PageTitle>
        <ImportButton />
      </div>
      {pages.isPending ? (
        <Placeholder />
      ) : !pages.data ? (
        <p className="mt-6 flex flex-wrap items-center gap-3 text-ui-lg">
          Couldn't load your library.
          <button type="button" onClick={() => void pages.refetch()} className={buttonClass}>
            Retry
          </button>
        </p>
      ) : documents.length === 0 && pending.length === 0 ? (
        <p className="mt-6 text-ink-2 text-ui-lg">
          No pages yet. Import a documentation page or a printed PDF to start.
        </p>
      ) : (
        <>
          <ul aria-label="Documents" className="mt-6 border-rule border-t">
            {pending.map((imp) => (
              <PendingRow key={imp.id} imp={imp} />
            ))}
            {documents.map((doc) => (
              <DocumentRow key={doc.id} doc={doc} />
            ))}
          </ul>
          {pages.isFetchNextPageError && (
            <p className="mt-4 text-ui-lg">Couldn't load more of your library.</p>
          )}
          {pages.hasNextPage && (
            <button
              type="button"
              onClick={() => void pages.fetchNextPage()}
              disabled={pages.isFetchingNextPage}
              className={`mt-4 ${buttonClass}`}
            >
              Load more
            </button>
          )}
        </>
      )}
    </div>
  );
}

function DocumentRow({ doc }: { doc: DocumentSummary }) {
  return (
    <li className={row}>
      <div className="min-w-0">
        <Link
          to="/doc/$id"
          params={{ id: doc.id }}
          className="block truncate font-medium text-ui-lg hover:underline"
        >
          {doc.title || hostOf(doc.source_url)}
        </Link>
        <p className="text-ink-2 text-ui">
          {hostOf(doc.source_url)} ·{" "}
          <time dateTime={doc.created_at}>{formatDate(doc.created_at)}</time>
        </p>
      </div>
    </li>
  );
}

/** An import from this session that has not become a document yet. */
function PendingRow({ imp }: { imp: TrackedImport }) {
  const tracker = useImportTracker();
  const ending = endingMessage(imp);
  return (
    <li className={row} data-import={imp.id}>
      <div className="min-w-0">
        <p className="truncate font-medium text-ui-lg">{imp.url}</p>
        <p className="text-ink-2 text-ui">
          {ending ?? (imp.status === "failed" ? "" : `${stepLabels[imp.status]}…`)}
        </p>
      </div>
      {ending && (
        <button type="button" onClick={() => tracker.dismiss(imp.id)} className={buttonClass}>
          Dismiss
        </button>
      )}
    </li>
  );
}

/** Static rows while the first page loads. */
function Placeholder() {
  return (
    <div className="mt-6">
      <p role="status" className="sr-only">
        Loading your library
      </p>
      <ul aria-hidden="true" className="border-rule border-t">
        {[0, 1, 2].map((n) => (
          <li key={n} className={row}>
            <div className="w-full space-y-2 py-1">
              <div className="h-3 w-2/3 rounded-sm bg-paper-2" />
              <div className="h-2.5 w-1/3 rounded-sm bg-paper-2" />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

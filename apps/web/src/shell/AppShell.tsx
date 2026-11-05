import { BooksIcon } from "@phosphor-icons/react/Books";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet } from "@tanstack/react-router";
import { useId } from "react";
import { recentDocuments } from "../api/queries";
import { Settings } from "./Settings";

/** Where a page can put an outline of itself, below the main navigation. */
export const outlineSlotId = "outline-slot";
/** Where a document's notes go in the notes margin (ADR 0007). */
export const notesSlotId = "notes-slot";

const navLink =
  "flex items-center gap-2 rounded-sm px-2 py-1 text-ui-lg text-ink-2 hover:bg-paper-2 hover:text-ink aria-[current=page]:bg-paper-2 aria-[current=page]:text-ink";

/**
 * The page frame: a 240px sidebar, the reading column and a notes margin.
 * Below the sidebar breakpoint the sidebar becomes a top bar; below the notes
 * breakpoint the margin is hidden.
 */
export function AppShell() {
  return (
    <div className="min-h-dvh sidebar:grid sidebar:grid-cols-[240px_minmax(0,1fr)]">
      <a
        href="#main"
        className="sr-only rounded-sm bg-paper px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        Skip to content
      </a>
      <header className="flex items-center gap-4 border-rule border-b px-4 py-2 sidebar:sticky sidebar:top-0 sidebar:h-dvh sidebar:flex-col sidebar:overflow-y-auto sidebar:items-stretch sidebar:gap-6 sidebar:border-r sidebar:border-b-0 sidebar:py-6">
        <Link to="/" className="px-2 font-semibold text-ui-lg">
          RoNote
        </Link>
        <nav aria-label="Main" className="min-w-0 flex-1">
          <ul className="flex gap-1 sidebar:flex-col">
            <li>
              <Link to="/" className={navLink}>
                <BooksIcon size={16} aria-hidden />
                Library
              </Link>
            </li>
          </ul>
          <RecentDocuments />
          {/* The document route puts its outline here. */}
          <div id={outlineSlotId} className="hidden sidebar:block" />
        </nav>
        <Settings />
      </header>
      <div className="min-w-0 notes:grid notes:grid-cols-[minmax(0,1fr)_280px]">
        <main id="main" tabIndex={-1} className="min-w-0 px-4 py-8 sidebar:px-12 sidebar:py-12">
          <Outlet />
        </main>
        <aside
          aria-label="Notes"
          className="hidden border-rule border-l px-6 py-12 text-ink-2 text-ui notes:block"
        >
          <div id={notesSlotId} className="peer" />
          <p className="peer-[:not(:empty)]:hidden">Notes appear here when you annotate a page.</p>
        </aside>
      </div>
    </div>
  );
}

/** The newest documents, under Library. The top bar has no room for them. */
function RecentDocuments() {
  const headingId = useId();
  const recent = useQuery(recentDocuments);
  if (!recent.data?.length) {
    return null;
  }
  return (
    <section aria-labelledby={headingId} className="mt-6 hidden sidebar:block">
      <h2 id={headingId} className="px-2 text-ink-2 text-ui">
        Recent
      </h2>
      <ul className="mt-1 space-y-0.5">
        {recent.data.map((doc) => (
          <li key={doc.id}>
            <Link to="/doc/$id" params={{ id: doc.id }} className={navLink}>
              <span className="truncate">{doc.title || doc.source_url}</span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

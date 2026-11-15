import type { Import, ImportErrorCode, ImportEvent, ImportStatus } from "../api/schema";

/** What the app knows about one import it follows. */
export interface TrackedImport {
  id: string;
  kind: Import["kind"];
  /** What is imported: the page's URL, or the PDF's filename. */
  name: string;
  status: ImportStatus;
  errorCode?: ImportErrorCode;
  documentId?: string;
  /** The event stream gave up before the import finished. */
  lost: boolean;
}

/** A step an import shows. */
export type Step = Exclude<ImportStatus, "failed">;

/** The steps an import of a kind shows, in order: a page is fetched, a PDF converted. */
export function stepsOf(kind: Import["kind"]): readonly Step[] {
  return kind === "pdf"
    ? ["queued", "converting", "extracting", "done"]
    : ["queued", "fetching", "extracting", "done"];
}

const order: Record<ImportStatus, number> = {
  queued: 0,
  fetching: 1,
  converting: 1,
  extracting: 2,
  done: 3,
  failed: 3,
};

export function tracked(imp: Import): TrackedImport {
  return applyEvent(
    {
      id: imp.id,
      kind: imp.kind,
      name: (imp.kind === "pdf" ? imp.filename : imp.requested_url) ?? "",
      status: "queued",
      lost: false,
    },
    {
      status: imp.status,
      ...(imp.error_code ? { error_code: imp.error_code } : {}),
      ...(imp.document_id ? { document_id: imp.document_id } : {}),
    },
  );
}

/**
 * Moves an import to the status an event reports. Status only moves forward:
 * after a reconnect the server repeats the current status, which changes
 * nothing, and a status that skips steps completes them.
 */
export function applyEvent(state: TrackedImport, event: ImportEvent): TrackedImport {
  if (order[event.status] <= order[state.status]) {
    return state;
  }
  return {
    ...state,
    status: event.status,
    ...(event.error_code ? { errorCode: event.error_code } : {}),
    ...(event.document_id ? { documentId: event.document_id } : {}),
  };
}

/** Records that the stream gave up; a finished import stays as it is. */
export function markLost(state: TrackedImport): TrackedImport {
  return isFinished(state) || state.lost ? state : { ...state, lost: true };
}

export function isFinished(state: TrackedImport): boolean {
  return state.status === "done" || state.status === "failed";
}

/** How far an import is along one of its steps. */
export function stepState(state: TrackedImport, step: Step) {
  if (state.status === "done" || order[step] < order[state.status]) return "complete";
  return step === state.status ? "current" : "waiting";
}

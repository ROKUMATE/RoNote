import { importErrorMessages, lostMessage, tooManyPagesMessage } from "../api/messages";
import type { Step, TrackedImport } from "./state";

export const stepLabels: Record<Step, string> = {
  queued: "Queued",
  fetching: "Fetching the page",
  converting: "Converting the PDF",
  extracting: "Extracting the content",
  done: "Done",
};

/** Why an import ended without a document, or undefined if it has not. */
export function endingMessage(state: TrackedImport): string | undefined {
  if (state.status !== "failed") return state.lost ? lostMessage : undefined;
  if (state.errorCode === "pdf_too_many_pages" && state.pdfPages) {
    return tooManyPagesMessage(state.pdfPages);
  }
  return importErrorMessages[state.errorCode ?? "internal_error"];
}

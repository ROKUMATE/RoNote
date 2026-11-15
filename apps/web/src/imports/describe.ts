import { importErrorMessages, lostMessage } from "../api/messages";
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
  if (state.status === "failed") return importErrorMessages[state.errorCode ?? "internal_error"];
  return state.lost ? lostMessage : undefined;
}

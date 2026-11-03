import { importErrorMessages, lostMessage } from "../api/messages";
import type { steps, TrackedImport } from "./state";

export const stepLabels: Record<(typeof steps)[number], string> = {
  queued: "Queued",
  fetching: "Fetching the page",
  extracting: "Extracting the content",
  done: "Done",
};

/** Why an import ended without a document, or undefined if it has not. */
export function endingMessage(state: TrackedImport): string | undefined {
  if (state.status === "failed") return importErrorMessages[state.errorCode ?? "internal_error"];
  return state.lost ? lostMessage : undefined;
}

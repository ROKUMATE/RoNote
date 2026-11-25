import { describe, expect, test } from "vitest";
import { maxPdfPages } from "../api/messages";
import { endingMessage } from "./describe";
import type { TrackedImport } from "./state";

const failed: TrackedImport = {
  id: "imp-1",
  kind: "pdf",
  name: "Book.pdf",
  status: "failed",
  errorCode: "pdf_too_many_pages",
  lost: false,
};

describe("a PDF over the page cap", () => {
  test("says how many pages it has, and the cap", () => {
    expect(endingMessage({ ...failed, pdfPages: 298 })).toBe(
      `That PDF has 298 pages, and RoNote imports at most ${maxPdfPages}.`,
    );
  });

  test("without its page count, says the cap", () => {
    expect(endingMessage(failed)).toBe(
      `That PDF has more than ${maxPdfPages} pages, the most RoNote imports.`,
    );
  });
});

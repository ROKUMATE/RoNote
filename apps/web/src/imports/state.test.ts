import { describe, expect, test } from "vitest";
import type { ImportEvent } from "../api/schema";
import { applyEvent, markLost, stepState, steps, type TrackedImport, tracked } from "./state";

const queued = tracked({
  id: "imp-1",
  requested_url: "https://go.dev/doc/",
  canonical_url: "https://go.dev/doc/",
  status: "queued",
  created_at: "2026-11-03T09:00:00Z",
  updated_at: "2026-11-03T09:00:00Z",
});

function play(...events: ImportEvent[]): TrackedImport {
  return events.reduce(applyEvent, queued);
}

function progress(state: TrackedImport) {
  return steps.map((step) => stepState(state, step));
}

describe("an import's state", () => {
  test("moves through the steps to done", () => {
    expect(progress(queued)).toEqual(["current", "waiting", "waiting", "waiting"]);
    expect(progress(play({ status: "fetching" }))).toEqual([
      "complete",
      "current",
      "waiting",
      "waiting",
    ]);
    expect(progress(play({ status: "fetching" }, { status: "extracting" }))).toEqual([
      "complete",
      "complete",
      "current",
      "waiting",
    ]);
    const done = play(
      { status: "fetching" },
      { status: "extracting" },
      { status: "done", document_id: "doc-1" },
    );
    expect(done).toMatchObject({ status: "done", documentId: "doc-1" });
    expect(progress(done)).toEqual(["complete", "complete", "complete", "complete"]);
  });

  test("ends with the code it failed with", () => {
    expect(
      play({ status: "fetching" }, { status: "failed", error_code: "not_found" }),
    ).toMatchObject({ status: "failed", errorCode: "not_found" });
  });

  test("ignores the repeated status a reconnect starts with", () => {
    const before = play({ status: "fetching" });
    expect(applyEvent(before, { status: "fetching" })).toBe(before);
    expect(play({ status: "fetching" }, { status: "fetching" }, { status: "extracting" })).toEqual(
      play({ status: "fetching" }, { status: "extracting" }),
    );
  });

  test("never moves back", () => {
    const extracting = play({ status: "extracting" });
    expect(applyEvent(extracting, { status: "fetching" })).toBe(extracting);
    const done = play({ status: "done", document_id: "doc-1" });
    expect(applyEvent(done, { status: "failed", error_code: "timeout" })).toBe(done);
  });

  test("completes the steps a reconnect skipped", () => {
    expect(progress(play({ status: "done", document_id: "doc-1" }))).toEqual([
      "complete",
      "complete",
      "complete",
      "complete",
    ]);
  });

  test("a duplicate import starts from the status the API gave", () => {
    const done = tracked({
      id: "imp-2",
      requested_url: "https://go.dev/doc/",
      canonical_url: "https://go.dev/doc/",
      status: "done",
      document_id: "doc-1",
      created_at: "2026-11-03T09:00:00Z",
      updated_at: "2026-11-03T09:00:00Z",
    });
    expect(done).toMatchObject({ status: "done", documentId: "doc-1" });
  });

  test("is lost only while it runs", () => {
    expect(markLost(queued).lost).toBe(true);
    const done = play({ status: "done", document_id: "doc-1" });
    expect(markLost(done)).toBe(done);
  });
});

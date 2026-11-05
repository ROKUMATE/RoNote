import { afterEach, beforeEach, expect, type Mock, test, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Annotation, DocContent, ReanchorRequest } from "../api/schema";
import { selectorOf } from "./anchoring";
import { ReanchorSync } from "./reanchorSync";

const original = "Install the tool before you start.";

function content(text: string): DocContent {
  return {
    doc_version: 1,
    blocks: [{ id: "p1", kind: "paragraph", content: [{ text }], origin: "user" }],
  };
}

function annotation(id: string, quote: string, version = 1): Annotation {
  const start = original.indexOf(quote);
  return {
    id,
    document_id: "doc-1",
    block_id: "p1",
    kind: "highlight",
    selector: selectorOf(original, start, start + quote.length),
    note: null,
    version,
    created_at: "2026-11-05T09:00:00Z",
    updated_at: "2026-11-05T09:00:00Z",
  };
}

let list: Annotation[];
type Moves = ReanchorRequest["annotations"];
let send: Mock<(moves: Moves) => Promise<Annotation[]>>;
let reload: Mock<() => Promise<void>>;
let sync: ReanchorSync;

beforeEach(() => {
  vi.useFakeTimers();
  list = [annotation("a1", "the tool", 3), annotation("a2", "start")];
  send = vi.fn(async (moves: Moves) =>
    moves.flatMap((m) => {
      const a = list.find((x) => x.id === m.id);
      return a ? [{ ...a, selector: m.selector, version: m.version + 1 }] : [];
    }),
  );
  reload = vi.fn(async () => {});
  sync = new ReanchorSync(content(original), {
    current: () => list,
    send,
    saved: (saved) => {
      list = list.map((a) => saved.find((s) => s.id === a.id) ?? a);
    },
    reload,
  });
});

afterEach(() => {
  sync.dispose();
  vi.useRealTimers();
});

test("after a save, the selectors that moved go in one batch, with their versions", async () => {
  sync.contentSaved(content(`First, ${original}`));
  sync.contentSaved(content(`First, ${original} Then stop.`));
  await vi.advanceTimersByTimeAsync(999);
  expect(send).not.toHaveBeenCalled();

  await vi.advanceTimersByTimeAsync(1);

  expect(send).toHaveBeenCalledTimes(1);
  const [moves = []] = send.mock.calls[0] ?? [];
  expect(moves.map((m) => [m.id, m.version, m.selector.position])).toEqual([
    ["a1", 3, { start: 15, end: 23 }],
    ["a2", 1, { start: 35, end: 40 }],
  ]);
});

test("a save that leaves the highlights where they were sends nothing", async () => {
  sync.contentSaved(content(original));
  await vi.advanceTimersByTimeAsync(1000);
  expect(send).not.toHaveBeenCalled();
});

test("a highlight whose words are gone stays as it was saved, unplaced", async () => {
  sync.contentSaved(content("Install it before you start."));
  await vi.advanceTimersByTimeAsync(1000);

  const [moves = []] = send.mock.calls[0] ?? [];
  expect(moves.map((m) => m.id)).toEqual(["a2"]);
});

test("a conflict reloads the annotations and tries again", async () => {
  send.mockRejectedValueOnce(new ApiError(409, undefined));
  sync.contentSaved(content(`First, ${original}`));
  await vi.advanceTimersByTimeAsync(1000);
  expect(reload).toHaveBeenCalledTimes(1);

  await vi.advanceTimersByTimeAsync(1000);

  expect(send).toHaveBeenCalledTimes(2);
});

test("a network failure retries with backoff; a refusal waits for the next save", async () => {
  send.mockRejectedValueOnce(new TypeError("offline"));
  sync.contentSaved(content(`First, ${original}`));
  await vi.advanceTimersByTimeAsync(1000);
  await vi.advanceTimersByTimeAsync(1000);
  expect(send).toHaveBeenCalledTimes(2);

  send.mockRejectedValueOnce(new ApiError(422, undefined));
  sync.contentSaved(content(`Second, ${original}`));
  await vi.advanceTimersByTimeAsync(1000);
  await vi.advanceTimersByTimeAsync(30_000);
  expect(send).toHaveBeenCalledTimes(3);
});

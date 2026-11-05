import { expect, test } from "vitest";
import { cutRuns, type Run } from "./codeMarks";

const tokens: Run[] = [
  { text: "func", color: "var(--code-token-keyword)" },
  { text: " " },
  { text: "main", color: "var(--code-token-function)" },
  { text: "()" },
  { text: "\n" },
  { text: "}" },
];

const joined = (runs: readonly Run[]) => runs.map((r) => r.text).join("");

test("no highlights leave the runs as they are", () => {
  expect(cutRuns(tokens, [])).toEqual(tokens);
});

test("a highlight across tokens cuts them at its edges, keeping colours and text", () => {
  const runs = cutRuns(tokens, [{ id: "a1", start: 2, end: 7 }]);

  expect(joined(runs)).toBe(joined(tokens));
  expect(runs).toEqual([
    { text: "fu", color: "var(--code-token-keyword)", mark: undefined },
    { text: "nc", color: "var(--code-token-keyword)", mark: "a1" },
    { text: " ", color: undefined, mark: "a1" },
    { text: "ma", color: "var(--code-token-function)", mark: "a1" },
    { text: "in", color: "var(--code-token-function)", mark: undefined },
    { text: "()", color: undefined, mark: undefined },
    { text: "\n", color: undefined, mark: undefined },
    { text: "}", color: undefined, mark: undefined },
  ]);
});

test("a highlight over a line break marks the break too, and plain text cuts the same way", () => {
  const runs = cutRuns([{ text: "a()\n}" }], [{ id: "a1", start: 1, end: 5 }]);

  expect(runs.map((r) => [r.text, r.mark])).toEqual([
    ["a", undefined],
    ["()\n}", "a1"],
  ]);
});

test("overlapping highlights cut at every edge", () => {
  const runs = cutRuns(
    [{ text: "abcdef" }],
    [
      { id: "a1", start: 0, end: 4 },
      { id: "a2", start: 2, end: 6 },
    ],
  );

  expect(joined(runs)).toBe("abcdef");
  expect(runs.map((r) => [r.text, r.mark])).toEqual([
    ["ab", "a1"],
    ["cd", "a1"],
    ["ef", "a2"],
  ]);
});

import { expect, test } from "vitest";
import { stack } from "./stack";

test("notes sit level with their highlights when there is room", () => {
  const tops = stack(
    [
      { id: "b", top: 400, height: 50 },
      { id: "a", top: 100, height: 50 },
    ],
    8,
  );
  expect([...tops]).toEqual([
    ["a", 100],
    ["b", 400],
  ]);
});

test("notes that would overlap are pushed below the one above, in order", () => {
  const tops = stack(
    [
      { id: "a", top: 100, height: 60 },
      { id: "b", top: 110, height: 40 },
      { id: "c", top: 150, height: 20 },
      { id: "d", top: 400, height: 20 },
    ],
    8,
  );
  expect([...tops]).toEqual([
    ["a", 100],
    ["b", 168],
    ["c", 216],
    ["d", 400],
  ]);
});

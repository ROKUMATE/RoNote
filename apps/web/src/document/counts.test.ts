import { expect, test } from "vitest";
import type { IrBlock } from "../api/schema";
import { blockCounts } from "./counts";

const code = (id: string): IrBlock => ({ id, kind: "code", text: "x\n" });
const paragraph = (id: string): IrBlock => ({ id, kind: "paragraph", content: [{ text: "x" }] });

test("counts blocks by kind, nested ones included", () => {
  const blocks: IrBlock[] = [
    { id: "h", kind: "heading", level: 1, content: [{ text: "Title" }] },
    paragraph("p1"),
    code("c1"),
    { id: "l", kind: "list", items: [{ children: [paragraph("p2"), code("c2")] }] },
    { id: "t", kind: "tabs", items: [{ label: "Go", children: [code("c3")] }] },
    { id: "n", kind: "callout", variant: "note", children: [code("c4")] },
  ];

  expect(blockCounts(blocks)).toEqual([
    "1 heading",
    "2 paragraphs",
    "1 list",
    "4 code blocks",
    "1 callout",
    "1 tab group",
  ]);
});

test("an empty document has no counts", () => {
  expect(blockCounts([])).toEqual([]);
});

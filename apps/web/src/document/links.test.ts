import { describe, expect, test } from "vitest";
import type { IrBlock } from "../api/schema";
import { linkStatus, sameDomainHrefs, splitFragment } from "./links";

const same = (href: string) => ({ text: href, link: { href, kind: "same_domain" as const } });

describe("sameDomainHrefs", () => {
  test("finds same-domain links anywhere, once each, and nothing else", () => {
    const blocks: IrBlock[] = [
      {
        id: "p",
        kind: "paragraph",
        content: [
          same("https://d.example/a"),
          { text: "x", link: { href: "https://o.example/", kind: "external" } },
        ],
      },
      {
        id: "l",
        kind: "list",
        items: [
          { children: [{ id: "lp", kind: "paragraph", content: [same("https://d.example/b")] }] },
        ],
      },
      {
        id: "t",
        kind: "table",
        head: [[[same("https://d.example/c")]]],
        rows: [[null, [same("https://d.example/a")]]],
      },
      {
        id: "tb",
        kind: "tabs",
        items: [
          {
            label: "x",
            children: [{ id: "tp", kind: "paragraph", content: [same("https://d.example/d")] }],
          },
        ],
      },
      {
        id: "i",
        kind: "image",
        src: "https://d.example/i.png",
        caption: [same("https://d.example/e")],
      },
      {
        id: "h",
        kind: "heading",
        level: 2,
        content: [{ text: "#top", link: { href: "#top", kind: "anchor" } }],
      },
    ];

    expect(sameDomainHrefs(blocks)).toEqual([
      "https://d.example/a",
      "https://d.example/b",
      "https://d.example/c",
      "https://d.example/d",
      "https://d.example/e",
    ]);
  });
});

describe("linkStatus", () => {
  const targets = new Map([
    ["https://d.example/a", "doc-1"],
    ["https://d.example/b", null],
  ]);

  test("a link to a document in the library is internal", () => {
    expect(linkStatus("https://d.example/a", targets)).toEqual({
      state: "internal",
      documentId: "doc-1",
    });
  });

  test("a link to a page not imported yet imports", () => {
    expect(linkStatus("https://d.example/b", targets)).toEqual({ state: "import" });
  });

  test("a link not resolved yet is unknown", () => {
    expect(linkStatus("https://d.example/c", targets)).toEqual({ state: "unknown" });
    expect(linkStatus("https://d.example/a", undefined)).toEqual({ state: "unknown" });
  });
});

test("splitFragment separates the page from the section", () => {
  expect(splitFragment("https://d.example/b?run=1#usage")).toEqual({
    url: "https://d.example/b?run=1",
    fragment: "usage",
  });
  expect(splitFragment("https://d.example/b")).toEqual({
    url: "https://d.example/b",
    fragment: "",
  });
});

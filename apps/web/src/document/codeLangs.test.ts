import { expect, test } from "vitest";
import type { DocContent, IrBlock } from "../api/schema";
import { specEnum } from "../test/spec";
import { codeLangs, unlabelled, withCodeLangs } from "./codeLangs";
import { languageOf } from "./languages";

const blocks: IrBlock[] = [
  { id: "c1", kind: "code", lang: "sh", text: "make\n" },
  {
    id: "l1",
    kind: "list",
    items: [
      {
        children: [
          { id: "p1", kind: "paragraph", content: [{ text: "Then:" }] },
          { id: "c2", kind: "code", text: "x := 1\n" },
        ],
      },
    ],
  },
  {
    id: "t1",
    kind: "tabs",
    items: [{ label: "Go", children: [{ id: "c3", kind: "code", text: "y\n" }] }],
  },
  { id: "c4", kind: "code", text: "z\n" },
];

test("the picker offers the spec's languages, in its order", () => {
  expect(codeLangs).toEqual(specEnum("CodeLang"));
});

test("every language but plain text highlights", () => {
  expect(codeLangs.filter((lang) => !languageOf(lang))).toEqual(["plaintext"]);
});

test("unlabelled code is code, at any depth, with no language from the page or the user", () => {
  expect(unlabelled(blocks, {})).toEqual(["c2", "c3", "c4"]);
  expect(unlabelled(blocks, { c3: "go" })).toEqual(["c2", "c4"]);
});

test("a save carries the choices for code still in the document, and none when there are none", () => {
  const content: DocContent = {
    doc_version: 1,
    blocks: blocks
      .filter((b) => b.id !== "c4")
      .map((b) => ({ ...b, origin: "source", edited: false, source_block_id: b.id })),
  };

  expect(withCodeLangs(content, { c2: "go", c4: "sql" }).code_langs).toEqual({ c2: "go" });
  expect(withCodeLangs(content, { c4: "sql" })).toBe(content);
  expect("code_langs" in withCodeLangs(content, {})).toBe(false);
});

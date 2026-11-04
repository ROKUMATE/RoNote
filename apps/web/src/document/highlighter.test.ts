import { expect, test } from "vitest";
import type { IrBlock } from "../api/schema";
import { highlight, tokensText } from "./highlighter";
import { languageOf, loadLanguage } from "./languages";

const goldens = import.meta.glob<{ blocks: IrBlock[] }>(
  "../../../../testdata/golden/*/expected.json",
  { eager: true, import: "default" },
);

function codeBlocks(blocks: IrBlock[]): IrBlock[] {
  return blocks.flatMap((b) => [
    ...(b.kind === "code" ? [b] : []),
    ...codeBlocks(b.children ?? []),
    ...codeBlocks((b.items ?? []).flatMap((i) => i.children)),
  ]);
}

// The verbatim guard on the highlighted path: highlighting wraps the code's
// text in spans and never changes a character of it.
test("highlighting spells out every golden code block exactly", { timeout: 60_000 }, async () => {
  let highlighted = 0;
  for (const golden of Object.values(goldens)) {
    for (const block of codeBlocks(golden.blocks)) {
      const language = languageOf(block.lang);
      if (!language) continue;
      const tokens = await highlight(block.text ?? "", language, await loadLanguage(language));
      expect(tokens, `${block.lang} block ${block.id}`).toBeDefined();
      expect(tokensText(tokens ?? [])).toBe(block.text);
      highlighted++;
    }
  }
  expect(highlighted).toBeGreaterThan(100);
});

test("a language RoNote has no grammar for stays plain", () => {
  expect(languageOf("brainfuck")).toBeUndefined();
  expect(languageOf(undefined)).toBeUndefined();
  expect(languageOf("py")).toBe("python");
  expect(languageOf("Go")).toBe("go");
});

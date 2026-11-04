import { createContext } from "react";
import type { CodeLang, DocContent, IrBlock } from "../api/schema";
import { eachBlock } from "./walk";

/*
 * The languages a user chooses for code blocks (ADR 0006): saved in the
 * document's code_langs, by block id, over the page's own. The code itself
 * never changes; only how it is highlighted does.
 */

/** The languages a user chose, by code block id. */
export type ChosenLangs = NonNullable<DocContent["code_langs"]>;

/** How the language picker names each language, in the spec's CodeLang order. */
export const codeLangLabels: Record<CodeLang, string> = {
  go: "Go",
  python: "Python",
  javascript: "JavaScript",
  typescript: "TypeScript",
  tsx: "TSX",
  jsx: "JSX",
  bash: "Bash",
  console: "Shell session",
  c: "C",
  cpp: "C++",
  csharp: "C#",
  java: "Java",
  kotlin: "Kotlin",
  rust: "Rust",
  swift: "Swift",
  ruby: "Ruby",
  php: "PHP",
  sql: "SQL",
  json: "JSON",
  yaml: "YAML",
  toml: "TOML",
  ini: "INI",
  xml: "XML",
  html: "HTML",
  css: "CSS",
  dockerfile: "Dockerfile",
  makefile: "Makefile",
  markdown: "Markdown",
  diff: "Diff",
  plaintext: "Plain text",
};

export const codeLangs = Object.keys(codeLangLabels) as CodeLang[];

/** The code blocks, nested ones included, with no language from the page and none chosen. */
export function unlabelled(blocks: readonly IrBlock[], chosen: ChosenLangs): string[] {
  const ids: string[] = [];
  eachBlock(blocks, (block) => {
    if (block.kind === "code" && !block.lang && !chosen[block.id]) ids.push(block.id);
  });
  return ids;
}

/**
 * content with the chosen languages of its code blocks. A block deleted in
 * the editor takes its choice out of the save; undo brings both back.
 */
export function withCodeLangs(content: DocContent, chosen: ChosenLangs): DocContent {
  const kept: ChosenLangs = {};
  eachBlock(content.blocks, (block) => {
    const lang = chosen[block.id];
    if (block.kind === "code" && lang) kept[block.id] = lang;
  });
  return Object.keys(kept).length === 0 ? content : { ...content, code_langs: kept };
}

/** The document's chosen languages, and how to change them. */
export type CodeLangChoices = {
  chosen: ChosenLangs;
  /** Sets block's language, or with undefined goes back to the page's. */
  choose: (blockId: string, lang: CodeLang | undefined) => void;
  /** Sets lang for every code block that has no language yet. */
  chooseForUnlabelled: (lang: CodeLang) => void;
};

/**
 * The editing document's code languages. Outside a document page there is
 * nowhere to save a choice, so the default keeps none.
 */
export const CodeLangsContext = createContext<CodeLangChoices>({
  chosen: {},
  choose: () => {},
  chooseForUnlabelled: () => {},
});

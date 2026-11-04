import type { LanguageInput } from "shiki/core";

/*
 * The languages RoNote highlights, each loaded on demand as its own chunk.
 * A block in any other language stays plain text.
 */
const languages = {
  bash: () => import("shiki/langs/bash.mjs"),
  c: () => import("shiki/langs/c.mjs"),
  cpp: () => import("shiki/langs/cpp.mjs"),
  csharp: () => import("shiki/langs/csharp.mjs"),
  css: () => import("shiki/langs/css.mjs"),
  diff: () => import("shiki/langs/diff.mjs"),
  docker: () => import("shiki/langs/docker.mjs"),
  go: () => import("shiki/langs/go.mjs"),
  html: () => import("shiki/langs/html.mjs"),
  ini: () => import("shiki/langs/ini.mjs"),
  java: () => import("shiki/langs/java.mjs"),
  javascript: () => import("shiki/langs/javascript.mjs"),
  json: () => import("shiki/langs/json.mjs"),
  jsx: () => import("shiki/langs/jsx.mjs"),
  kotlin: () => import("shiki/langs/kotlin.mjs"),
  markdown: () => import("shiki/langs/markdown.mjs"),
  php: () => import("shiki/langs/php.mjs"),
  python: () => import("shiki/langs/python.mjs"),
  ruby: () => import("shiki/langs/ruby.mjs"),
  rust: () => import("shiki/langs/rust.mjs"),
  shellsession: () => import("shiki/langs/shellsession.mjs"),
  sql: () => import("shiki/langs/sql.mjs"),
  swift: () => import("shiki/langs/swift.mjs"),
  toml: () => import("shiki/langs/toml.mjs"),
  tsx: () => import("shiki/langs/tsx.mjs"),
  typescript: () => import("shiki/langs/typescript.mjs"),
  xml: () => import("shiki/langs/xml.mjs"),
  yaml: () => import("shiki/langs/yaml.mjs"),
} satisfies Record<string, () => Promise<{ default: LanguageInput }>>;

export type Language = keyof typeof languages;

/** Names pages give languages, for the ones that differ from Shiki's. */
const aliases: Record<string, Language> = {
  sh: "bash",
  shell: "bash",
  zsh: "bash",
  console: "shellsession",
  shellscript: "bash",
  "c++": "cpp",
  cs: "csharp",
  dockerfile: "docker",
  golang: "go",
  js: "javascript",
  mjs: "javascript",
  md: "markdown",
  py: "python",
  python3: "python",
  rb: "ruby",
  rs: "rust",
  ts: "typescript",
  yml: "yaml",
};

/** The language a block's lang names, or undefined when RoNote has no grammar for it. */
export function languageOf(lang: string | undefined): Language | undefined {
  const name = lang?.toLowerCase();
  if (!name) return undefined;
  if (name in languages) return name as Language;
  return aliases[name];
}

/** Loads a language's grammar. */
export async function loadLanguage(language: Language): Promise<LanguageInput> {
  return (await languages[language]()).default;
}

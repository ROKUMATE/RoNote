import {
  createCssVariablesTheme,
  createHighlighterCore,
  type LanguageInput,
  type ThemedToken,
} from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";

/*
 * Loaded on demand as its own chunk, once a code block nears the viewport.
 * The theme's colours are CSS variables, which reader.css maps to six tokens.
 */

const theme = createCssVariablesTheme({
  name: "ronote",
  variablePrefix: "--code-",
  fontStyle: false,
});

const highlighter = createHighlighterCore({
  themes: [theme],
  langs: [],
  engine: createJavaScriptRegexEngine({ forgiving: true }),
});

/**
 * The code's tokens, line by line, or undefined when they would not spell
 * out the code exactly: highlighting only ever wraps the verbatim text.
 */
export async function highlight(
  code: string,
  language: string,
  grammar: LanguageInput,
): Promise<ThemedToken[][] | undefined> {
  const h = await highlighter;
  if (!h.getLoadedLanguages().includes(language)) {
    await h.loadLanguage(grammar);
  }
  const { tokens } = h.codeToTokens(code, { lang: language, theme: "ronote" });
  return tokensText(tokens) === code ? tokens : undefined;
}

/** The text the tokens spell out. */
export function tokensText(lines: ThemedToken[][]): string {
  return lines.map((line) => line.map((t) => t.content).join("")).join("\n");
}

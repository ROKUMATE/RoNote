/*
 * Per-browser display preferences. Storage can be missing or throw (private
 * mode, blocked site data), so every access is guarded and falls back to the
 * default. index.html applies the saved values before first paint with an
 * inline copy of the keys and values below.
 */

export type Theme = "system" | "light" | "dark";
export type ReaderFont = "serif" | "sans";

const themes: readonly Theme[] = ["system", "light", "dark"];
const readerFonts: readonly ReaderFont[] = ["serif", "sans"];

const themeKey = "ronote.theme";
const readerFontKey = "ronote.readerFont";

function read<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const saved = localStorage.getItem(key);
    return allowed.find((value) => value === saved) ?? fallback;
  } catch {
    return fallback;
  }
}

function save(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage is blocked: the choice still applies until the page reloads.
  }
}

/** Sets attribute on root, or removes it when value is the default. */
function apply(root: HTMLElement, attribute: string, value: string, fallback: string): void {
  if (value === fallback) {
    root.removeAttribute(attribute);
  } else {
    root.setAttribute(attribute, value);
  }
}

export const readTheme = (): Theme => read(themeKey, themes, "system");
export const saveTheme = (theme: Theme): void => save(themeKey, theme);
export const applyTheme = (theme: Theme, root = document.documentElement): void =>
  apply(root, "data-theme", theme, "system");

export const readReaderFont = (): ReaderFont => read(readerFontKey, readerFonts, "serif");
export const saveReaderFont = (font: ReaderFont): void => save(readerFontKey, font);
export const applyReaderFont = (font: ReaderFont, root = document.documentElement): void =>
  apply(root, "data-reader-font", font, "serif");

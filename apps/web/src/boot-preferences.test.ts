import { afterEach, expect, test, vi } from "vitest";
import indexHtml from "../index.html?raw";
import { saveReaderFont, saveTheme } from "./preferences";

// The inline script in index.html that applies preferences before first paint.
const bootScript = /<script id="boot-preferences">([\s\S]*?)<\/script>/.exec(indexHtml)?.[1] ?? "";

function runBootScript() {
  new Function(bootScript)();
}

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("data-reader-font");
});

test("index.html has the boot script", () => {
  expect(bootScript).toContain("localStorage");
});

test.each([
  { theme: "dark", font: "sans", wantTheme: "dark", wantFont: "sans" },
  { theme: "light", font: "serif", wantTheme: "light", wantFont: null },
  { theme: "system", font: "serif", wantTheme: null, wantFont: null },
] as const)(
  "applies what preferences.ts saved: $theme, $font",
  ({ theme, font, wantTheme, wantFont }) => {
    saveTheme(theme);
    saveReaderFont(font);

    runBootScript();

    expect(document.documentElement.getAttribute("data-theme")).toBe(wantTheme);
    expect(document.documentElement.getAttribute("data-reader-font")).toBe(wantFont);
  },
);

test("keeps the defaults when storage throws", () => {
  vi.stubGlobal("localStorage", {
    getItem: () => {
      throw new DOMException("blocked", "SecurityError");
    },
  });

  expect(runBootScript).not.toThrow();
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
});

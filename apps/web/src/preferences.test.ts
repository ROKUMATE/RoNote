import { afterEach, describe, expect, test, vi } from "vitest";
import {
  applyReaderFont,
  applyTheme,
  readReaderFont,
  readTheme,
  saveReaderFont,
  saveTheme,
} from "./preferences";

function memoryStorage(entries: Record<string, string> = {}) {
  const data = new Map(Object.entries(entries));
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
  };
}

const throwingStorage = {
  getItem: () => {
    throw new DOMException("blocked", "SecurityError");
  },
  setItem: () => {
    throw new DOMException("blocked", "SecurityError");
  },
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("read", () => {
  const cases = [
    { name: "theme, nothing saved", read: readTheme, storage: memoryStorage(), want: "system" },
    {
      name: "theme, saved",
      read: readTheme,
      storage: memoryStorage({ "ronote.theme": "dark" }),
      want: "dark",
    },
    {
      name: "theme, invalid saved value",
      read: readTheme,
      storage: memoryStorage({ "ronote.theme": "purple" }),
      want: "system",
    },
    { name: "theme, storage throwing", read: readTheme, storage: throwingStorage, want: "system" },
    {
      name: "reader font, nothing saved",
      read: readReaderFont,
      storage: memoryStorage(),
      want: "serif",
    },
    {
      name: "reader font, saved",
      read: readReaderFont,
      storage: memoryStorage({ "ronote.readerFont": "sans" }),
      want: "sans",
    },
    {
      name: "reader font, invalid saved value",
      read: readReaderFont,
      storage: memoryStorage({ "ronote.readerFont": "comic" }),
      want: "serif",
    },
    {
      name: "reader font, storage throwing",
      read: readReaderFont,
      storage: throwingStorage,
      want: "serif",
    },
  ];

  test.each(cases)("$name", ({ read, storage, want }) => {
    vi.stubGlobal("localStorage", storage);
    expect(read()).toBe(want);
  });
});

describe("save", () => {
  test("theme round-trips through storage", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    saveTheme("light");
    expect(readTheme()).toBe("light");
  });

  test("reader font round-trips through storage", () => {
    vi.stubGlobal("localStorage", memoryStorage());
    saveReaderFont("sans");
    expect(readReaderFont()).toBe("sans");
  });

  test("does not throw when storage throws", () => {
    vi.stubGlobal("localStorage", throwingStorage);
    expect(() => saveTheme("dark")).not.toThrow();
    expect(() => saveReaderFont("sans")).not.toThrow();
  });
});

describe("apply", () => {
  const cases = [
    {
      name: "dark theme",
      start: {},
      apply: (r: HTMLElement) => applyTheme("dark", r),
      attribute: "data-theme",
      want: "dark",
    },
    {
      name: "system theme",
      start: { "data-theme": "dark" },
      apply: (r: HTMLElement) => applyTheme("system", r),
      attribute: "data-theme",
      want: null,
    },
    {
      name: "sans reader font",
      start: {},
      apply: (r: HTMLElement) => applyReaderFont("sans", r),
      attribute: "data-reader-font",
      want: "sans",
    },
    {
      name: "serif reader font",
      start: { "data-reader-font": "sans" },
      apply: (r: HTMLElement) => applyReaderFont("serif", r),
      attribute: "data-reader-font",
      want: null,
    },
  ];

  test.each(cases)("$name", ({ start, apply, attribute, want }) => {
    const element = root(start);
    apply(element);
    expect(element.getAttribute(attribute)).toBe(want);
  });
});

function root(attributes: Record<string, string> = {}): HTMLElement {
  const element = document.createElement("html");
  for (const [name, value] of Object.entries(attributes)) {
    element.setAttribute(name, value);
  }
  return element;
}

// Applies saved preferences before first paint, as a classic script in the
// head so it blocks rendering, and from a file so the app's CSP allows no
// inline script. Keys and values mirror src/preferences.ts.
try {
  const root = document.documentElement;
  const theme = localStorage.getItem("ronote.theme");
  if (theme === "light" || theme === "dark") root.setAttribute("data-theme", theme);
  if (localStorage.getItem("ronote.readerFont") === "sans")
    root.setAttribute("data-reader-font", "sans");
} catch {}

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { browsers } from "./browsers.ts";

// Tailwind's own optimizer targets older browsers than ./browsers and would
// rewrite light-dark(), so Vite minifies the CSS instead.

// The API runs as its own process (make run-api). Vite's proxy streams
// responses as they come, so the import progress events pass through.
const proxy = {
  "/api": { target: process.env.RONOTE_API_ORIGIN ?? "http://127.0.0.1:8080" },
};

// Where the runner page is served (vite.runner.config.ts).
const runnerOrigin = process.env.RONOTE_RUNNER_ORIGIN ?? "http://localhost:5174";

// Cross-origin isolated, so the runner's frame may be too and Stop can
// interrupt Python. credentialless still loads documents' images from their
// own sites, which send no CORP header (docs/adr/0008-in-browser-code-execution.md).
const headers = {
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Embedder-Policy": "credentialless",
};

export default defineConfig({
  plugins: [react(), tailwindcss({ optimize: false })],
  define: { "import.meta.env.RONOTE_RUNNER_ORIGIN": JSON.stringify(runnerOrigin) },
  build: { target: browsers, manifest: true },
  server: { proxy, headers },
  preview: { proxy, headers },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/test-setup.ts"],
    // Vitest's on-disk module cache keys a test's build by the test file's
    // own text, but an import.meta.glob in it also depends on which files
    // exist: a file added later went unread until the test changed (P11's
    // link test broke the spec-type guard unseen). Building takes ~2% of
    // the run.
    fsModuleCache: false,
    // Tuning hints after every run are noise when nothing failed.
    experimental: { diagnostics: false },
  },
});

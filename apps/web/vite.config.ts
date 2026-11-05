import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The oldest browsers with both the popover API and CSS light-dark(), which
// the app relies on. Tailwind's own optimizer targets older browsers and would
// rewrite light-dark(), so Vite minifies the CSS instead.
const browsers = ["chrome123", "edge123", "firefox125", "safari17.5"];

// The API runs as its own process (make run-api). Vite's proxy streams
// responses as they come, so the import progress events pass through.
const proxy = {
  "/api": { target: process.env.RONOTE_API_ORIGIN ?? "http://127.0.0.1:8080" },
};

export default defineConfig({
  plugins: [react(), tailwindcss({ optimize: false })],
  build: { target: browsers, manifest: true },
  server: { proxy },
  preview: { proxy },
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
  },
});

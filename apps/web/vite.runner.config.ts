import { defineConfig } from "vite";
import { browsers } from "./browsers.ts";

/*
 * The runner origin (docs/adr/0008-in-browser-code-execution.md): its own
 * server, port and build, never part of the app's. It is served from
 * localhost while the app is on 127.0.0.1, since cookies ignore ports and
 * would otherwise be shared.
 */

// The app's origins, which alone may frame the runner and talk to it.
const appOrigins = (process.env.RONOTE_APP_ORIGINS ?? "http://127.0.0.1:5173")
  .split(/[\s,]+/)
  .filter(Boolean);

const headers = {
  "Content-Security-Policy": [
    "default-src 'self'",
    "script-src 'self' 'wasm-unsafe-eval'",
    "connect-src 'self'",
    `frame-ancestors ${appOrigins.join(" ")}`,
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; "),
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Embedder-Policy": "require-corp",
  "Cross-Origin-Resource-Policy": "cross-origin",
};

export default defineConfig({
  root: "runner",
  define: { "import.meta.env.RONOTE_APP_ORIGINS": JSON.stringify(appOrigins) },
  build: {
    target: browsers,
    outDir: "../dist-runner",
    emptyOutDir: true,
  },
  server: { port: 5174, strictPort: true, headers },
  preview: { port: 5174, strictPort: true, headers },
});

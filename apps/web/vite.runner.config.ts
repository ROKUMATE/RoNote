import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { defineConfig, type Plugin } from "vite";
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

const pyodideDir = dirname(createRequire(import.meta.url).resolve("pyodide/package.json"));

/** The Pyodide files the runner serves under /pyodide/, and their types. */
const pyodideFiles: Record<string, string> = {
  "pyodide.asm.mjs": "text/javascript",
  "pyodide.asm.wasm": "application/wasm",
  "python_stdlib.zip": "application/zip",
  "pyodide-lock.json": "application/json",
};

function pyodideFile(name: string): string | Buffer {
  if (name !== "pyodide-lock.json") return readFileSync(join(pyodideDir, name));
  // Its own lock file, listing no packages: none can load, and a missing
  // module's error suggests none.
  const lock = JSON.parse(readFileSync(join(pyodideDir, name), "utf8"));
  return JSON.stringify({ info: lock.info, packages: {} });
}

/** Serves Pyodide from node_modules in dev, and copies it into the build. */
function pyodide(): Plugin {
  return {
    name: "ronote-pyodide",
    configureServer(server) {
      server.middlewares.use("/pyodide", (request, response, next) => {
        const name = request.url?.split("?")[0]?.slice(1) ?? "";
        const type = pyodideFiles[name];
        if (!type) return next();
        for (const [header, value] of Object.entries(headers)) response.setHeader(header, value);
        response.setHeader("Content-Type", type);
        response.end(pyodideFile(name));
      });
    },
    generateBundle() {
      for (const name of Object.keys(pyodideFiles)) {
        this.emitFile({ type: "asset", fileName: `pyodide/${name}`, source: pyodideFile(name) });
      }
    },
  };
}

export default defineConfig({
  root: "runner",
  plugins: [pyodide()],
  // Pyodide finds its files from indexURL, not from where Vite would put it.
  optimizeDeps: { exclude: ["pyodide"] },
  worker: { format: "es" },
  define: { "import.meta.env.RONOTE_APP_ORIGINS": JSON.stringify(appOrigins) },
  build: {
    target: browsers,
    outDir: "../dist-runner",
    emptyOutDir: true,
  },
  server: { port: 5174, strictPort: true, headers },
  preview: { port: 5174, strictPort: true, headers },
});

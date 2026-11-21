import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { type Connect, defineConfig, type Plugin } from "vite";
import { browsers } from "./browsers.ts";
import { headersFor as runnerHeaders, workerScripts } from "./runner-headers.ts";
import { origins } from "./vite.origins.ts";

/*
 * The runner origin (docs/adr/0008-in-browser-code-execution.md): its own
 * server, port and build, never part of the app's. It is served from
 * localhost while the app is on 127.0.0.1, since cookies ignore ports and
 * would otherwise be shared.
 */

// The app's origin, which alone may frame the runner and talk to it: in
// dev, where make web-dev serves the app, unless RONOTE_PUBLIC_URL says
// otherwise. Caddy and Cloudflare Pages send the same headers, from the
// same definition (runner-headers.ts).
const devAppURL = "http://127.0.0.1:5173";
const appOrigin = new URL(process.env.RONOTE_PUBLIC_URL ?? devAppURL).origin;

// The dev server serves the JavaScript worker's script from its source.
const workers = [...workerScripts, "/javascript.worker.ts"];

/** The runner's headers for a response to path, on the dev and preview servers. */
export function headersFor(path: string): Record<string, string> {
  return runnerHeaders(path, appOrigin, workers);
}

/** Sets every response's headers by its path, in dev and in preview. */
function headers(): Plugin {
  const set: Connect.NextHandleFunction = (request, response, next) => {
    const path = request.url?.split("?")[0] ?? "/";
    for (const [name, value] of Object.entries(headersFor(path))) response.setHeader(name, value);
    next();
  };
  return {
    name: "ronote-runner-headers",
    configureServer(server) {
      server.middlewares.use(set);
    },
    configurePreviewServer(server) {
      server.middlewares.use(set);
    },
  };
}

const pyodidePackage = createRequire(import.meta.url).resolve("pyodide/package.json");
const pyodideDir = dirname(pyodidePackage);

// Pyodide's files keep their names, so their path carries the version: a
// new Pyodide is a new URL, and the files can be cached for good.
const pyodidePath = `pyodide/${JSON.parse(readFileSync(pyodidePackage, "utf8")).version}`;
const pyodideURL = `/${pyodidePath}/`;

/** The Pyodide files the runner serves under pyodideURL, and their types. */
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
      server.middlewares.use(`/${pyodidePath}`, (request, response, next) => {
        const name = request.url?.split("?")[0]?.slice(1) ?? "";
        const type = pyodideFiles[name];
        if (!type) return next();
        response.setHeader("Content-Type", type);
        response.end(pyodideFile(name));
      });
    },
    generateBundle() {
      for (const name of Object.keys(pyodideFiles)) {
        this.emitFile({
          type: "asset",
          fileName: `${pyodidePath}/${name}`,
          source: pyodideFile(name),
        });
      }
    },
  };
}

export default defineConfig({
  root: "runner",
  // Its own, not the app's: two dev servers sharing one cache each rebuild
  // it for themselves, and a worker loading a dependency the other just
  // replaced fails to start.
  cacheDir: "../node_modules/.vite-runner",
  plugins: [headers(), pyodide(), origins({ RONOTE_PUBLIC_URL: devAppURL })],
  // Pyodide finds its files from indexURL, not from where Vite would put it.
  optimizeDeps: { exclude: ["pyodide"] },
  worker: { format: "es" },
  define: { "import.meta.env.RONOTE_PYODIDE_URL": JSON.stringify(pyodideURL) },
  build: {
    target: browsers,
    outDir: "../dist-runner",
    emptyOutDir: true,
  },
  server: { port: 5174, strictPort: true },
  preview: { port: 5174, strictPort: true },
});

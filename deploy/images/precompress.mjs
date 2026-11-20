// Writes a gzip and a Brotli copy beside each static file Caddy serves
// precompressed (file_server precompressed), so no request compresses one:
// Pyodide's 9.6 MB of WebAssembly goes out as about 3 MB. HTML is left as
// is, since Caddy fills its templates per request.
//
//   node precompress.mjs DIR...
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { brotliCompressSync, constants, gzipSync } from "node:zlib";

const compressible = /\.(js|mjs|css|json|wasm|svg|zip|txt|map)$/;

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path);
    else if (compressible.test(name)) compress(path);
  }
}

function compress(path) {
  const data = readFileSync(path);
  writeFileSync(`${path}.gz`, gzipSync(data, { level: 9 }));
  writeFileSync(
    `${path}.br`,
    brotliCompressSync(data, {
      params: {
        [constants.BROTLI_PARAM_QUALITY]: 11,
        [constants.BROTLI_PARAM_SIZE_HINT]: data.length,
      },
    }),
  );
}

for (const dir of process.argv.slice(2)) walk(dir);

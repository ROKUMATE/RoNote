/*
 * The runner's response headers (docs/adr/0008-in-browser-code-execution.md,
 * docs/adr/0015-operations.md), defined once for every server that sends
 * them: the dev server (vite.runner.config.ts), Caddy in CI and make
 * prod-local (deploy/caddy/runner.caddy, generated) and Cloudflare Pages in
 * production (its _headers, written at deploy). Every response keeps code
 * from reaching any other host; only the JavaScript worker's script may
 * evaluate code, which running the reader's JavaScript takes, and its
 * worker lives by that policy, not the page's.
 */

/** The built JavaScript worker's script; a trailing * is a prefix. */
export const workerScripts = ["/assets/javascript.worker-*"];

/** Every response's headers but the CSP. */
const common: Record<string, string> = {
  "Strict-Transport-Security": "max-age=63072000; includeSubDomains",
  "X-Content-Type-Options": "nosniff",
  "Permissions-Policy":
    "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
  "Referrer-Policy": "no-referrer",
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Embedder-Policy": "require-corp",
  "Cross-Origin-Resource-Policy": "cross-origin",
};

/** Files that never change under their path: hashed names, Pyodide's version. */
const immutable = ["/assets/*", "/pyodide/*"];
const forever = "public, max-age=31536000, immutable";

/** The CSP for a response, framed by appOrigin alone. */
function csp(appOrigin: string, evaluates: boolean): string {
  return [
    "default-src 'self'",
    `script-src 'self' ${evaluates ? "'unsafe-eval'" : "'wasm-unsafe-eval'"}`,
    "connect-src 'self'",
    `frame-ancestors ${appOrigin}`,
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}

/** Whether path matches pattern, where a trailing * matches any rest. */
export function matches(pattern: string, path: string): boolean {
  return pattern.endsWith("*") ? path.startsWith(pattern.slice(0, -1)) : path === pattern;
}

/** The headers of the response to path; workers lists the worker's paths. */
export function headersFor(
  path: string,
  appOrigin: string,
  workers: string[] = workerScripts,
): Record<string, string> {
  const evaluates = workers.some((pattern) => matches(pattern, path));
  const headers: Record<string, string> = {
    ...common,
    "Content-Security-Policy": csp(appOrigin, evaluates),
  };
  if (immutable.some((pattern) => matches(pattern, path))) headers["Cache-Control"] = forever;
  return headers;
}

/**
 * Caddy's site for the runner, with the app's origin and the runner's own
 * from Caddy's environment; static is the Caddyfile's snippet that serves
 * the files.
 */
export function caddyRunnerSite(): string {
  const app = "{$RONOTE_PUBLIC_URL}";
  const workers = workerScripts.join(" ");
  const header = Object.entries(common)
    .map(([name, value]) => `\t\t${name} "${value}"`)
    .join("\n");
  return `# Generated from apps/web/runner-headers.ts by make generate: edit that.
# The runner (docs/adr/0008-in-browser-code-execution.md), in CI and make
# prod-local; production's is on Cloudflare Pages (docs/adr/0015-operations.md).
{$RONOTE_RUNNER_URL} {
\theader {
${header}
\t\t-Server
\t}
\t@worker path ${workers}
\t@not-worker not path ${workers}
\theader @worker Content-Security-Policy "${csp(app, true)}"
\theader @not-worker Content-Security-Policy "${csp(app, false)}"
\t@immutable path ${immutable.join(" ")}
\theader @immutable Cache-Control "${forever}"
\troot * /srv/runner
\timport static
}
`;
}

/**
 * The headers Cloudflare Pages sends for path under _headers text: every
 * rule whose path matches, in order; a header set again is joined to the
 * first with a comma, and "! name" removes what earlier rules set
 * (developers.cloudflare.com/pages/configuration/headers).
 */
export function pagesHeadersFor(text: string, path: string): Record<string, string> {
  const headers: Record<string, string> = {};
  let applies = false;
  for (const line of text.split("\n")) {
    if (line.trim() === "" || line.startsWith("#")) continue;
    if (!line.startsWith(" ")) {
      applies = matches(line.trim(), path);
      continue;
    }
    if (!applies) continue;
    const entry = line.trim();
    if (entry.startsWith("! ")) {
      delete headers[entry.slice(2)];
      continue;
    }
    const colon = entry.indexOf(": ");
    const name = entry.slice(0, colon);
    const value = entry.slice(colon + 2);
    headers[name] = name in headers ? `${headers[name]}, ${value}` : value;
  }
  return headers;
}

/**
 * Cloudflare Pages' _headers for the runner, framed by appOrigin. Pages
 * applies every rule whose path matches, in order, joining a header set
 * twice with a comma, and "! name" removes what earlier rules set: so the
 * worker's own CSP comes in a rule after the one that removes the page's.
 */
export function pagesHeaders(appOrigin: string): string {
  const rule = (path: string, lines: string[]) =>
    [path, ...lines.map((line) => `  ${line}`)].join("\n");
  const set = (headers: Record<string, string>) =>
    Object.entries(headers).map(([name, value]) => `${name}: ${value}`);
  return [
    "# Generated from apps/web/runner-headers.ts by scripts/runner-pages.ts.",
    rule("/*", set({ ...common, "Content-Security-Policy": csp(appOrigin, false) })),
    ...immutable.map((path) => rule(path, set({ "Cache-Control": forever }))),
    ...workerScripts.map((path) => rule(path, ["! Content-Security-Policy"])),
    ...workerScripts.map((path) =>
      rule(path, set({ "Content-Security-Policy": csp(appOrigin, true) })),
    ),
    "",
  ].join("\n");
}

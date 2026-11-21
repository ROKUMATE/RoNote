import { cpSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { matches, pagesHeaders, pagesHeadersFor, workerScripts } from "./runner-headers.ts";
import { fill } from "./vite.origins.ts";

/*
 * The runner's Cloudflare Pages site (docs/adr/0015-operations.md), made at
 * deploy from its build: the files, index.html naming the app, as Caddy
 * fills it elsewhere, and Pages' _headers. It checks what it made, so a
 * release never ships a placeholder or a file without exactly one CSP.
 */

/** Every file under dir, as the path the site serves it at. */
function paths(dir: string): string[] {
  return readdirSync(dir, { recursive: true, encoding: "utf8" })
    .filter((name) => statSync(join(dir, name)).isFile())
    .map((name) => `/${relative(dir, join(dir, name)).split(sep).join("/")}`);
}

/** Why the site in dir is wrong for appOrigin, if it is. */
function problems(dir: string, appOrigin: string): string[] {
  const out: string[] = [];
  const headers = readFileSync(join(dir, "_headers"), "utf8");
  for (const path of paths(dir)) {
    if (path === "/_headers") continue;
    if (path.endsWith(".html") && readFileSync(join(dir, path), "utf8").includes("{{env")) {
      out.push(`${path} still has a placeholder`);
    }
    const csp = pagesHeadersFor(headers, path)["Content-Security-Policy"] ?? "";
    if (csp.split("default-src").length !== 2)
      out.push(`${path} gets ${csp ? "more than one CSP" : "no CSP"}`);
    const worker = workerScripts.some((pattern) => matches(pattern, path));
    if (csp.includes("'unsafe-eval'") !== worker) {
      out.push(`${path} ${worker ? "lacks" : "gets"} 'unsafe-eval'`);
    }
    if (!csp.includes(`frame-ancestors ${appOrigin};`))
      out.push(`${path} isn't framed by ${appOrigin} alone`);
  }
  return out;
}

/** Makes the site in to from the runner's build in from, for the app at appURL. */
export function buildPages(from: string, to: string, appURL: string): void {
  const app = new URL(appURL);
  if (app.protocol !== "https:" || app.origin !== appURL.replace(/\/$/, "")) {
    throw new Error(`the app's URL must be an https origin, not ${appURL}`);
  }
  rmSync(to, { recursive: true, force: true });
  cpSync(from, to, { recursive: true });
  for (const path of paths(to).filter((path) => path.endsWith(".html"))) {
    const file = join(to, path);
    writeFileSync(
      file,
      fill(readFileSync(file, "utf8"), (name) => {
        if (name !== "RONOTE_PUBLIC_URL")
          throw new Error(`${path} names ${name}, which Pages can't fill`);
        return app.origin;
      }),
    );
  }
  writeFileSync(join(to, "_headers"), pagesHeaders(app.origin));
  const wrong = problems(to, app.origin);
  if (wrong.length > 0) throw new Error(`the runner's Pages site is wrong:\n${wrong.join("\n")}`);
}

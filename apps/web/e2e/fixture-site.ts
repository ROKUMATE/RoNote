import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { setTimeout as sleep } from "node:timers/promises";
import { stack } from "./stack.ts";

/*
 * The site the end-to-end tests import from. It serves the extractor's
 * golden fixtures:
 *   /robots.txt           lets everyone in except to /private/
 *   /pages/<fixture>/     testdata/golden/<fixture>/input.html
 *   /slow/<fixture>/      the same, 3s later, so an import can be watched
 *   /private/<fixture>/   the same, but robots.txt keeps RoNote out
 * and, for following links, two pages of its own that link to each other:
 *   /linked/<page>/       e2e/fixtures/<page>.html, each {{run}} replaced by
 *                         the request's run parameter, so a test's links
 *                         lead to pages no other test has imported; page B
 *                         answers late, so following a link can be watched
 *   /files/sample.pdf     a PDF, which RoNote does not import
 * Anything else answers 404. Query strings are otherwise ignored, so every
 * test imports a URL of its own.
 */

const golden = new URL("../../../testdata/golden/", import.meta.url);
const linked = new URL("./fixtures/", import.meta.url);
const slowDelay = 3000;

const server = createServer(async (request, response) => {
  const { pathname, searchParams } = new URL(request.url ?? "/", stack.fixtures);
  if (pathname === "/robots.txt") {
    response.writeHead(200, { "Content-Type": "text/plain" });
    response.end("User-agent: *\nDisallow: /private/\n");
    return;
  }
  const [, kind, fixture] = /^\/(pages|slow|private)\/([a-z0-9-]+)\/$/.exec(pathname) ?? [];
  if (kind && fixture) {
    const page = await readFile(new URL(`${fixture}/input.html`, golden)).catch(() => undefined);
    if (page) {
      if (kind === "slow") await sleep(slowDelay);
      response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      response.end(page);
      return;
    }
  }
  const [, page] = /^\/linked\/([a-z0-9-]+)\/$/.exec(pathname) ?? [];
  if (page) {
    const html = await readFile(new URL(`${page}.html`, linked), "utf8").catch(() => undefined);
    if (html) {
      if (page === "linked-b") await sleep(slowDelay);
      response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      response.end(html.replaceAll("{{run}}", encodeURIComponent(searchParams.get("run") ?? "")));
      return;
    }
  }
  if (pathname === "/files/sample.pdf") {
    response.writeHead(200, { "Content-Type": "application/pdf" });
    response.end("%PDF-1.4\n%%EOF\n");
    return;
  }
  response.writeHead(404, { "Content-Type": "text/plain" });
  response.end("not found\n");
});

const { hostname, port } = new URL(stack.fixtures);
server.listen(Number(port), hostname, () => {
  console.log(`fixture site on ${stack.fixtures}`);
});

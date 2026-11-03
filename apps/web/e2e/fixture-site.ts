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
 * Anything else answers 404. Query strings are ignored, so every test
 * imports a URL of its own.
 */

const golden = new URL("../../../testdata/golden/", import.meta.url);
const slowDelay = 3000;

const server = createServer(async (request, response) => {
  const { pathname } = new URL(request.url ?? "/", stack.fixtures);
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
  response.writeHead(404, { "Content-Type": "text/plain" });
  response.end("not found\n");
});

const { hostname, port } = new URL(stack.fixtures);
server.listen(Number(port), hostname, () => {
  console.log(`fixture site on ${stack.fixtures}`);
});

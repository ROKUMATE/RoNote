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
 *   /long/                a page of 60 sections, each a long paragraph and
 *                         30 lines of code, the 55th about "zanzibar": its
 *                         layout moves as its font arrives and its code
 *                         renders
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
  if (pathname === "/long/") {
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    response.end(longPage());
    return;
  }
  if (pathname === "/files/sample.pdf") {
    response.writeHead(200, { "Content-Type": "application/pdf" });
    response.end("%PDF-1.4\n%%EOF\n");
    return;
  }
  response.writeHead(404, { "Content-Type": "text/plain" });
  response.end("not found\n");
});

/** /long/: sections whose code renders much taller than its placeholder. */
function longPage(): string {
  const sections = Array.from({ length: 60 }, (_, i) => {
    const topic = i === 54 ? "zanzibar spice routes" : `topic number ${i + 1}`;
    const code = Array.from(
      { length: 30 },
      (_, line) => `step_${i}_${line} = compute(${line})`,
    ).join("\n");
    // Long paragraphs wrap differently once the document's own font arrives.
    const prose = `This section is about ${topic}. ${"It explains the idea step by step, with the reasons behind each choice and the mistakes students most often make along the way. ".repeat(6)}`;
    return `<h2>Section ${i + 1}</h2>\n<p>${prose}</p>\n<pre><code class="language-python">${code}</code></pre>`;
  });
  return `<!doctype html><html><head><title>Long guide</title></head><body><main><h1>Long guide</h1>\n${sections.join("\n")}</main></body></html>`;
}

const { hostname, port } = new URL(stack.fixtures);
server.listen(Number(port), hostname, () => {
  console.log(`fixture site on ${stack.fixtures}`);
});

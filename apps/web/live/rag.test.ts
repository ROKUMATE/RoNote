import { execFileSync } from "node:child_process";
import { expect, test } from "vitest";
import type { IrBlock } from "../src/api/schema";
import { classifyAll, codeBlocks } from "../src/document/run/classify";

/*
 * The real RAG tutorial, extracted over the network by the Go extractor,
 * classified as the reader would see it. Its code needs packages, keys and
 * the network: none of it may offer a plain Run. make test-live only.
 */
const tutorial = "https://myengineeringpath.dev/programming/python/rag-pipeline-tutorial/";

test("no block of the RAG tutorial runs as is", () => {
  const ir: { blocks: IrBlock[] } = JSON.parse(
    execFileSync("go", ["run", "./cmd/extract", "--format=json", tutorial], {
      cwd: new URL("../../..", import.meta.url),
      encoding: "utf8",
      maxBuffer: 64 << 20,
    }),
  );
  const blocks = codeBlocks(ir.blocks);
  const found = classifyAll(blocks);
  const classes = blocks.map((b) => `${b.lang ?? "-"}: ${found.get(b.id)?.class}`);
  const runs = blocks.filter((b) => found.get(b.id)?.run === "run");

  expect(classes.length).toBeGreaterThan(5);
  expect(runs.map((b) => `${b.lang ?? "-"}: ${found.get(b.id)?.class}`)).toEqual([]);
});

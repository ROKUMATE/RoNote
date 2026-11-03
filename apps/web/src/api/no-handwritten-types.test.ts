import { describe, expect, test } from "vitest";
import { allSpecEnums, specSchemas, specSnakeProperties } from "../test/spec";

// Every source file except the generated types and this test, which holds
// bad samples on purpose.
const sources = import.meta.glob<string>(
  ["../**/*.{ts,tsx}", "!./schema.d.ts", "!./no-handwritten-types.test.ts"],
  { query: "?raw", import: "default", eager: true },
);

const schemaNames = new Set(Object.keys(specSchemas).map((name) => name.toLowerCase()));
const enums = [...allSpecEnums()];
const snakeProperties = [...specSnakeProperties()];

/**
 * Lists the ways source re-declares an API type instead of importing it from
 * schema.d.ts: a type, interface or enum named like a spec schema; a union
 * of string literals from one spec enum; or a type annotation of two or more
 * of one spec schema's snake_case properties.
 */
function findHandwrittenApiTypes(source: string): string[] {
  const found: string[] = [];
  for (const [, keyword, name] of source.matchAll(
    /^\s*(?:export\s+)?(?:declare\s+)?(type|interface|enum)\s+([A-Za-z_$][\w$]*)/gm,
  )) {
    if (name && schemaNames.has(name.toLowerCase()))
      found.push(`${keyword} ${name} copies a spec schema`);
  }
  for (const [union] of source.matchAll(/(["'])[\w-]+\1(?:\s*\|\s*(["'])[\w-]+\2)+/g)) {
    const members = [...union.matchAll(/["']([\w-]+)["']/g)].map((m) => m[1]);
    for (const [where, values] of enums) {
      if (members.filter((m) => m !== undefined && values.includes(m)).length >= 2) {
        found.push(`${union} copies the enum ${where}`);
      }
    }
  }
  const annotated = new Set(
    [...source.matchAll(/\b([a-z]+(?:_[a-z]+)+)\??\s*:\s*(?:string|number|boolean)\b/g)].map(
      (m) => m[1],
    ),
  );
  for (const [where, names] of snakeProperties) {
    const copied = names.filter((name) => annotated.has(name));
    if (copied.length >= 2) found.push(`a type with ${copied.join(", ")} copies ${where}`);
  }
  return found;
}

describe("findHandwrittenApiTypes", () => {
  test("finds each kind of copy", () => {
    const bad = `
      type ImportStatus = "queued" | "fetching" | "done";
      interface Row { source_url: string; created_at: string }
    `;
    expect(findHandwrittenApiTypes(bad)).toEqual([
      "type ImportStatus copies a spec schema",
      '"queued" | "fetching" | "done" copies the enum ImportStatus',
      "a type with source_url, created_at copies DocumentSummary",
    ]);
  });

  test("leaves imports, aliases of other names and data alone", () => {
    const good = `
      import type { Import, ImportStatus } from "./schema";
      import { type Document, api } from "./client";
      type Step = { status: ImportStatus; label: string };
      const doc = { source_url: "https://go.dev/", created_at: "2026-11-03T00:00:00Z" };
      const flag = mode === "queued" ? 1 : 2;
    `;
    expect(findHandwrittenApiTypes(good)).toEqual([]);
  });
});

test("no source file hand-writes a type the API spec defines", () => {
  expect(Object.keys(sources).length).toBeGreaterThan(5);
  const found = Object.entries(sources).flatMap(([path, source]) =>
    findHandwrittenApiTypes(source).map((finding) => `${path}: ${finding}`),
  );
  expect(found).toEqual([]);
});

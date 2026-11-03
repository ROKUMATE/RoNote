import { afterEach, expect, test, vi } from "vitest";
import { apiError, json, stubApi } from "../test/api";
import { ApiError } from "./client";
import { createImport } from "./queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

const anImport = {
  id: "0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10",
  requested_url: "https://go.dev/doc/",
  canonical_url: "https://go.dev/doc/",
  status: "queued",
  created_at: "2026-11-03T09:00:00Z",
  updated_at: "2026-11-03T09:00:00Z",
};

test("a queued import is new", async () => {
  const requests = stubApi({ "POST /imports": () => json({ import: anImport }, 202) });

  await expect(createImport("https://go.dev/doc/")).resolves.toEqual({
    existing: false,
    import: anImport,
  });
  expect(await requests[0]?.json()).toEqual({ url: "https://go.dev/doc/" });
});

test("a page the user already has comes with its document", async () => {
  const document = {
    id: "0192f1c4-0000-7000-8000-000000000002",
    title: "Documentation",
    source_url: "https://go.dev/doc/",
    version: 1,
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
  };
  const done = { ...anImport, status: "done", document_id: document.id };
  stubApi({ "POST /imports": () => json({ import: done, document }) });

  await expect(createImport("https://go.dev/doc/")).resolves.toEqual({
    existing: true,
    import: done,
    document,
  });
});

test("an error envelope becomes an ApiError with its code", async () => {
  stubApi({ "POST /imports": () => apiError(400, "ip_literal_host") });

  const error = await createImport("http://10.0.0.1/").catch((e: unknown) => e);

  expect(error).toBeInstanceOf(ApiError);
  expect(error).toMatchObject({ status: 400, body: { code: "ip_literal_host" } });
});

test("a response that is not the envelope still becomes an ApiError", async () => {
  stubApi({ "POST /imports": () => new Response("Bad Gateway", { status: 502 }) });

  await expect(createImport("https://go.dev/")).rejects.toMatchObject({
    status: 502,
    body: undefined,
  });
});

import { afterEach, expect, test, vi } from "vitest";
import { apiError, json, stubApi } from "../test/api";
import {
  createQueryClient,
  documentPages,
  documentQuery,
  documentsKey,
  linkTargets,
  recentDocuments,
} from "./queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

function summary(n: number) {
  return {
    id: `doc-${n}`,
    title: `Doc ${n}`,
    source_url: `https://docs.example.com/${n}`,
    version: 1,
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
  };
}

test("documents come a cursor page at a time", async () => {
  const requests = stubApi({
    "GET /documents": (request) =>
      new URL(request.url).searchParams.get("cursor") === "c1"
        ? json({ documents: [summary(3)] })
        : json({ documents: [summary(1), summary(2)], next_cursor: "c1" }),
  });
  const client = createQueryClient();

  const { pages, pageParams } = await client.fetchInfiniteQuery({ ...documentPages, pages: 3 });

  expect(pages.flatMap((p) => p.documents.map((d) => d.id))).toEqual(["doc-1", "doc-2", "doc-3"]);
  expect(pageParams).toEqual(["", "c1"]);
  expect(requests.map((r) => new URL(r.url).search)).toEqual(["?limit=20", "?limit=20&cursor=c1"]);
});

test("the sidebar asks for the newest eight", async () => {
  const requests = stubApi({ "GET /documents": () => json({ documents: [summary(1)] }) });

  await expect(createQueryClient().fetchQuery(recentDocuments)).resolves.toEqual([summary(1)]);
  expect(requests.map((r) => new URL(r.url).search)).toEqual(["?limit=8"]);
});

test("one document is fetched by its ID, and a missing one is not retried", async () => {
  const requests = stubApi({
    "GET /documents/doc-1": () => json({ ...summary(1), content: {} }),
    "GET /documents/gone": () => apiError(404, "not_found"),
  });
  const client = createQueryClient();

  await expect(client.fetchQuery(documentQuery("doc-1"))).resolves.toMatchObject({ id: "doc-1" });
  await expect(client.fetchQuery(documentQuery("gone"))).rejects.toMatchObject({ status: 404 });
  expect(requests).toHaveLength(2);
});

test("one invalidation refreshes every documents query", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => json({ ...summary(1), content: {} }),
  });
  const client = createQueryClient();
  await client.fetchInfiniteQuery(documentPages);
  await client.fetchQuery(recentDocuments);
  await client.fetchQuery(documentQuery("doc-1"));

  await client.invalidateQueries({ queryKey: documentsKey });

  const stale = client
    .getQueryCache()
    .getAll()
    .map((q) => q.isStale());
  expect(stale).toEqual([true, true, true]);
});

test("link targets are resolved 200 URLs at a time", async () => {
  const hrefs = Array.from({ length: 450 }, (_, i) => `https://docs.example.com/${i}`);
  const requests = stubApi({
    "POST /documents/resolve": async (request) => {
      const { urls } = (await request.clone().json()) as { urls: string[] };
      return json({
        links: urls.map((url) => (url.endsWith("/7") ? { url, document_id: "doc-7" } : { url })),
      });
    },
  });

  const targets = await createQueryClient().fetchQuery(linkTargets("doc-1", hrefs));

  const sizes = await Promise.all(
    requests.map(async (r) => ((await r.json()) as { urls: string[] }).urls.length),
  );
  expect(sizes).toEqual([200, 200, 50]);
  expect(targets.size).toBe(450);
  expect(targets.get("https://docs.example.com/7")).toBe("doc-7");
  expect(targets.get("https://docs.example.com/8")).toBeNull();
});

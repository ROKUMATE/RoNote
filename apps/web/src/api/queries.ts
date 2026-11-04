import { infiniteQueryOptions, QueryClient, queryOptions } from "@tanstack/react-query";
import { ApiError, api, dataOrThrow } from "./client";
import type { DocContent, Document, DocumentSummary, Import } from "./schema";

/** Every documents query sits under this key, so one invalidation refreshes them all. */
export const documentsKey = ["documents"] as const;

const pageSize = 20;
const recentCount = 8;

/** The user's documents, newest first, a cursor page at a time. */
export const documentPages = infiniteQueryOptions({
  queryKey: [...documentsKey, "pages"],
  queryFn: async ({ pageParam }) =>
    dataOrThrow(
      await api.GET("/documents", {
        params: { query: { limit: pageSize, ...(pageParam ? { cursor: pageParam } : {}) } },
      }),
    ),
  initialPageParam: "",
  getNextPageParam: (page) => page.next_cursor,
});

/** The newest few documents, for the sidebar. */
export const recentDocuments = queryOptions({
  queryKey: [...documentsKey, "recent"],
  queryFn: async () =>
    dataOrThrow(await api.GET("/documents", { params: { query: { limit: recentCount } } }))
      .documents,
});

/** One document with its content. */
export function documentQuery(id: string) {
  return queryOptions({
    queryKey: [...documentsKey, "one", id],
    queryFn: async () =>
      dataOrThrow(await api.GET("/documents/{id}", { params: { path: { id } } })),
  });
}

/**
 * Saves a document's content, edited from version; resolves to the new
 * version. keepalive lets the save finish while the page is going away.
 */
export async function saveDocument(
  id: string,
  content: DocContent,
  version: number,
  { keepalive }: { keepalive: boolean },
): Promise<number> {
  const saved = dataOrThrow(
    await api.PATCH("/documents/{id}", {
      params: { path: { id } },
      body: { content, version },
      keepalive,
    }),
  );
  return saved.version;
}

/** Puts one source block of a document back as extracted; returns the document. */
export async function revertBlock(id: string, blockId: string): Promise<Document> {
  const { document } = dataOrThrow(
    await api.POST("/documents/{id}/blocks/{blockId}/revert", {
      params: { path: { id, blockId } },
    }),
  );
  return document;
}

/**
 * What asking to import a URL did: queued a new import, or found the
 * document the user already has for it.
 */
export type CreatedImport =
  | { existing: false; import: Import }
  | { existing: true; import: Import; document: DocumentSummary };

export async function createImport(url: string): Promise<CreatedImport> {
  const created = dataOrThrow(await api.POST("/imports", { body: { url } }));
  return "document" in created
    ? { existing: true, import: created.import, document: created.document }
    : { existing: false, import: created.import };
}

/**
 * The app's query client. A client error (4xx) will not change on retry, so
 * only server and network failures are retried, once.
 */
export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        retry: (failures, error) =>
          failures < 1 && !(error instanceof ApiError && error.status < 500),
      },
    },
  });
}

import { infiniteQueryOptions, QueryClient, queryOptions } from "@tanstack/react-query";
import { ApiError, api, dataOrThrow, okOrThrow } from "./client";
import type {
  Annotation,
  AnnotationPatch,
  DocContent,
  Document,
  DocumentSummary,
  Import,
  NewAnnotation,
  ReanchorRequest,
} from "./schema";

/** The signed-in user. */
export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: async () => dataOrThrow(await api.GET("/me")),
  staleTime: Number.POSITIVE_INFINITY,
});

/** Ends this browser's session. */
export async function signOut(): Promise<void> {
  okOrThrow(await api.POST("/auth/logout"));
}

/** Ends every session of the user, in every browser. */
export async function signOutEverywhere(): Promise<void> {
  okOrThrow(await api.POST("/auth/logout-all"));
}

/** Deletes the user's account; confirm is their GitHub login. */
export async function deleteAccount(confirm: string): Promise<void> {
  okOrThrow(await api.DELETE("/me", { body: { confirm } }));
}

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

/** How many URLs one resolve request may carry, as the spec allows. */
const resolveBatch = 200;

/**
 * Where a document's same-domain links lead: for each href, the user's
 * document for it, or null when they have none. Under the documents key,
 * so a finished import refreshes it.
 */
export function linkTargets(id: string, hrefs: readonly string[]) {
  return queryOptions({
    queryKey: [...documentsKey, "links", id],
    enabled: hrefs.length > 0,
    queryFn: async () => {
      const found = new Map<string, string | null>();
      for (let i = 0; i < hrefs.length; i += resolveBatch) {
        const urls = hrefs.slice(i, i + resolveBatch);
        const { links } = dataOrThrow(await api.POST("/documents/resolve", { body: { urls } }));
        for (const link of links) found.set(link.url, link.document_id ?? null);
      }
      return found;
    },
  });
}

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

/** A document's highlights and notes, oldest first (ADR 0007). */
export function annotationsQuery(documentId: string) {
  return queryOptions({
    queryKey: [...documentsKey, "annotations", documentId],
    queryFn: async () =>
      dataOrThrow(
        await api.GET("/documents/{id}/annotations", { params: { path: { id: documentId } } }),
      ).annotations,
  });
}

export async function createAnnotation(
  documentId: string,
  body: NewAnnotation,
): Promise<Annotation> {
  return dataOrThrow(
    await api.POST("/documents/{id}/annotations", { params: { path: { id: documentId } }, body }),
  );
}

export async function updateAnnotation(id: string, body: AnnotationPatch): Promise<Annotation> {
  return dataOrThrow(await api.PATCH("/annotations/{id}", { params: { path: { id } }, body }));
}

export async function deleteAnnotation(id: string): Promise<void> {
  okOrThrow(await api.DELETE("/annotations/{id}", { params: { path: { id } } }));
}

/** Saves new selectors for a document's annotations, all or none. */
export async function reanchorAnnotations(
  documentId: string,
  annotations: ReanchorRequest["annotations"],
): Promise<Annotation[]> {
  return dataOrThrow(
    await api.POST("/documents/{id}/annotations/reanchor", {
      params: { path: { id: documentId } },
      body: { annotations },
    }),
  ).annotations;
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

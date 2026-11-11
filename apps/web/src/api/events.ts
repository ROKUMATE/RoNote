import { api, apiBase } from "./client";
import type { ImportEvent } from "./schema";

const finalStatuses: ReadonlySet<ImportEvent["status"]> = new Set(["done", "failed"]);

/**
 * Follows an import's progress over server-sent events, calling onEvent for
 * each status, and closes the stream once the import is done or failed. When
 * the connection drops the browser reconnects by itself, and the server
 * starts again with the current status. onLost is called if the browser
 * gives up instead, as when the server refuses the stream. An EventSource
 * can't see why it was refused, so it then asks the API who is signed in: a
 * session that ended answers 401, and the client goes to sign in. The
 * returned function stops following.
 */
export function subscribeImport(
  id: string,
  onEvent: (event: ImportEvent) => void,
  onLost?: () => void,
): () => void {
  const source = new EventSource(`${apiBase}/imports/${encodeURIComponent(id)}/events`);
  source.addEventListener("status", (message) => {
    const event: ImportEvent = JSON.parse(message.data);
    if (finalStatuses.has(event.status)) {
      source.close();
    }
    onEvent(event);
  });
  source.addEventListener("error", () => {
    if (source.readyState === EventSource.CLOSED) {
      void api.GET("/me").catch(() => {});
      onLost?.();
    }
  });
  return () => source.close();
}

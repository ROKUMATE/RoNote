import { apiBase } from "./client";
import type { ImportEvent } from "./schema";

const finalStatuses: ReadonlySet<ImportEvent["status"]> = new Set(["done", "failed"]);

/**
 * Follows an import's progress over server-sent events, calling onEvent for
 * each status, and closes the stream once the import is done or failed. When
 * the connection drops the browser reconnects by itself, and the server
 * starts again with the current status. onLost is called if the browser
 * gives up instead, as when the server refuses the stream. The returned
 * function stops following.
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
      onLost?.();
    }
  });
  return () => source.close();
}

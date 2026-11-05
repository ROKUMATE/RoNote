import { ApiError } from "../api/client";
import type { Annotation, DocContent, ReanchorRequest } from "../api/schema";
import { reanchor, sameSelector, textOf } from "./anchoring";
import { retryDelay } from "./save";

/** How long re-anchored selectors wait after a save, so several saves send one batch. */
const batchMs = 1000;

type Moves = ReanchorRequest["annotations"];

/**
 * Keeps saved selectors on their words (docs/adr/0007-annotations-and-anchoring.md).
 * After each content save, every annotation is re-anchored against the
 * content just stored, the one text the server checks quotes against; the
 * selectors that moved are saved in one batch, apart from the content.
 */
export class ReanchorSync {
  private stored: DocContent;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private retries = 0;
  private readonly options: {
    /** The annotations as the app has them now. */
    current: () => readonly Annotation[];
    send: (moves: Moves) => Promise<Annotation[]>;
    /** Takes in the annotations as saved. */
    saved: (annotations: Annotation[]) => void;
    /** Reloads the annotations, after another tab changed one. */
    reload: () => Promise<void>;
  };

  constructor(content: DocContent, options: ReanchorSync["options"]) {
    this.stored = content;
    this.options = options;
  }

  /** The content was stored: re-anchor against it shortly. */
  contentSaved(content: DocContent) {
    this.stored = content;
    this.schedule(batchMs);
  }

  /** Re-anchor against the content as last stored, shortly. */
  check() {
    this.schedule(batchMs);
  }

  dispose() {
    clearTimeout(this.timer);
  }

  private schedule(ms: number) {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), ms);
  }

  private async flush() {
    const moves = this.options.current().flatMap((a) => {
      const text = textOf(this.stored, a.block_id);
      const next = text === undefined ? undefined : reanchor(a.selector, text);
      return next && !sameSelector(next, a.selector)
        ? [{ id: a.id, selector: next, version: a.version }]
        : [];
    });
    if (moves.length === 0) return;
    try {
      this.options.saved(await this.options.send(moves));
      this.retries = 0;
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        await this.options.reload();
        this.schedule(batchMs);
      } else if (!(error instanceof ApiError && error.status < 500)) {
        this.schedule(retryDelay(this.retries++));
      }
      // Any other refusal means the text moved on again: the next save
      // re-anchors against it.
    }
  }
}

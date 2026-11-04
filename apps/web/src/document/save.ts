import { ApiError } from "../api/client";
import type { DocContent, ErrorCode } from "../api/schema";

/*
 * Autosave (docs/adr/0006-editor-and-doc-content.md): a save 1s after the
 * last change, and at once on blur or when the page is hidden. Each save
 * names the version it was edited from. A conflict stops saving for good:
 * this tab never overwrites a newer version.
 */

export type SaveStatus =
  | { state: "idle" }
  | { state: "dirty" }
  | { state: "saving" }
  | { state: "saved" }
  | { state: "offline" }
  | { state: "conflict" }
  | { state: "refused"; code: ErrorCode | undefined };

/** Saves content edited from version; resolves to the new version. */
export type SaveFn = (
  content: DocContent,
  version: number,
  options: { keepalive: boolean },
) => Promise<number>;

const debounceMs = 1000;
const retryMs = [1000, 2000, 5000, 10_000, 30_000];

export class Autosave {
  private version: number;
  private status: SaveStatus = { state: "idle" };
  private timer: ReturnType<typeof setTimeout> | undefined;
  private inFlight: Promise<void> | undefined;
  private changedSinceSave = false;
  private retries = 0;
  private readonly save: SaveFn;
  private readonly read: () => DocContent;
  private readonly onStatus: (status: SaveStatus) => void;

  constructor(options: {
    version: number;
    save: SaveFn;
    /** The content to save, read when a save starts. */
    read: () => DocContent;
    onStatus: (status: SaveStatus) => void;
  }) {
    this.version = options.version;
    this.save = options.save;
    this.read = options.read;
    this.onStatus = options.onStatus;
  }

  /** The content changed: save once it has been still for a second. */
  change() {
    if (this.stopped()) return;
    this.changedSinceSave = true;
    this.set({ state: "dirty" });
    this.schedule(debounceMs);
  }

  /** Saves pending changes now; resolves once nothing is pending. */
  async flush({ keepalive = false } = {}): Promise<void> {
    if (this.stopped()) return;
    await this.inFlight;
    if (this.changedSinceSave && !this.stopped()) await this.run(keepalive);
  }

  /** The server moved the document on itself, as a revert does. */
  setVersion(version: number) {
    this.version = version;
  }

  /** Stops any timer; for when the document closes. */
  dispose() {
    clearTimeout(this.timer);
  }

  private stopped() {
    return this.status.state === "conflict" || this.status.state === "refused";
  }

  private schedule(ms: number) {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), ms);
  }

  private set(status: SaveStatus) {
    this.status = status;
    this.onStatus(status);
  }

  private run(keepalive: boolean): Promise<void> {
    clearTimeout(this.timer);
    this.changedSinceSave = false;
    this.set({ state: "saving" });
    const attempt = this.save(this.read(), this.version, { keepalive }).then(
      (version) => {
        this.version = version;
        this.retries = 0;
        if (this.changedSinceSave) {
          this.set({ state: "dirty" });
          this.schedule(debounceMs);
        } else {
          this.set({ state: "saved" });
        }
      },
      (error: unknown) => {
        if (error instanceof ApiError && error.status === 409) {
          this.set({ state: "conflict" });
        } else if (error instanceof ApiError && error.status < 500) {
          this.set({ state: "refused", code: error.body?.code });
        } else {
          // Offline, or the server failed: keep the changes and try again.
          this.changedSinceSave = true;
          this.set({ state: "offline" });
          this.schedule(retryMs[Math.min(this.retries, retryMs.length - 1)] ?? debounceMs);
          this.retries++;
        }
      },
    );
    this.inFlight = attempt.finally(() => {
      this.inFlight = undefined;
    });
    return this.inFlight;
  }
}

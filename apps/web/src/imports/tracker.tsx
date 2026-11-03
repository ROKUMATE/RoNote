import type { QueryClient } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useSyncExternalStore } from "react";
import { subscribeImport } from "../api/events";
import { documentsKey } from "../api/queries";
import type { Import } from "../api/schema";
import { applyEvent, isFinished, markLost, type TrackedImport, tracked } from "./state";

/**
 * The imports this session follows. They live outside React, so closing the
 * dialog or leaving the page never stops one; each is followed until it is
 * done or failed. When one is done, every documents query is invalidated.
 */
export class ImportTracker {
  private readonly queryClient: QueryClient;
  private readonly imports = new Map<string, TrackedImport>();
  private readonly unsubscribes = new Map<string, () => void>();
  private readonly listeners = new Set<() => void>();
  private snapshot: readonly TrackedImport[] = [];

  constructor(queryClient: QueryClient) {
    this.queryClient = queryClient;
  }

  /** Starts following an import the API just queued. */
  track(imp: Import) {
    if (this.imports.has(imp.id)) return;
    this.set(tracked(imp));
    this.unsubscribes.set(
      imp.id,
      subscribeImport(
        imp.id,
        (event) => this.update(imp.id, (state) => applyEvent(state, event)),
        () => this.update(imp.id, markLost),
      ),
    );
  }

  /** Forgets an import, and stops following it if it is still running. */
  dismiss(id: string) {
    this.stop(id);
    if (this.imports.delete(id)) this.emit();
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = () => this.snapshot;

  private update(id: string, change: (state: TrackedImport) => TrackedImport) {
    const state = this.imports.get(id);
    if (!state) return;
    const next = change(state);
    if (next === state) return;
    if (isFinished(next) || next.lost) this.stop(id);
    if (next.status === "done") {
      void this.queryClient.invalidateQueries({ queryKey: documentsKey });
    }
    this.set(next);
  }

  private set(state: TrackedImport) {
    this.imports.set(state.id, state);
    this.emit();
  }

  private stop(id: string) {
    this.unsubscribes.get(id)?.();
    this.unsubscribes.delete(id);
  }

  private emit() {
    this.snapshot = [...this.imports.values()];
    for (const listener of this.listeners) listener();
  }
}

const TrackerContext = createContext<ImportTracker | null>(null);

export function ImportsProvider({
  tracker,
  children,
}: {
  tracker: ImportTracker;
  children: ReactNode;
}) {
  return <TrackerContext value={tracker}>{children}</TrackerContext>;
}

export function useImportTracker(): ImportTracker {
  const tracker = useContext(TrackerContext);
  if (!tracker) throw new Error("useImportTracker needs an ImportsProvider");
  return tracker;
}

/** The imports this session follows, oldest first. */
export function useTrackedImports(): readonly TrackedImport[] {
  const tracker = useImportTracker();
  return useSyncExternalStore(tracker.subscribe, tracker.getSnapshot);
}

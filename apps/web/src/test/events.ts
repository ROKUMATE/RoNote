import { act } from "@testing-library/react";
import { vi } from "vitest";
import type { ImportEvent } from "../api/schema";

/**
 * Stands in for ../api/events, the seam at the event stream: a test mocks
 * that module with this one, and plays the server with send.
 */
export const subscriptions: {
  id: string;
  onEvent: (event: ImportEvent) => void;
  unsubscribe: () => void;
}[] = [];

export const subscribeImport = vi.fn((id: string, onEvent: (event: ImportEvent) => void) => {
  const subscription = { id, onEvent, unsubscribe: vi.fn() };
  subscriptions.push(subscription);
  return subscription.unsubscribe;
});

/** The server sends event to the newest subscription. */
export function send(event: ImportEvent) {
  const subscription = subscriptions.at(-1);
  if (!subscription) throw new Error("nothing subscribed");
  act(() => subscription.onEvent(event));
}

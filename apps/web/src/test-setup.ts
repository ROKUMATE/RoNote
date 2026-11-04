import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { FakeIntersectionObserver } from "./test/intersection";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// jsdom has no <dialog> behaviour. This stand-in opens and closes one and
// fires close, which is all the app relies on; Playwright covers the rest.
if (!HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    if (!this.open) return;
    this.open = false;
    this.dispatchEvent(new Event("close"));
  };
}

// The router restores scroll positions, which jsdom does not implement.
window.scrollTo = () => {};

// jsdom has no IntersectionObserver; nothing intersects unless a test says so.
globalThis.IntersectionObserver =
  FakeIntersectionObserver as unknown as typeof IntersectionObserver;

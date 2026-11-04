/**
 * Stands in for IntersectionObserver, which jsdom lacks. Nothing intersects
 * until a test calls bringIntoView.
 */
export class FakeIntersectionObserver {
  static all: FakeIntersectionObserver[] = [];
  readonly observed = new Set<Element>();
  readonly callback: IntersectionObserverCallback;

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
    FakeIntersectionObserver.all.push(this);
  }

  observe(el: Element) {
    this.observed.add(el);
  }

  unobserve(el: Element) {
    this.observed.delete(el);
  }

  disconnect() {
    this.observed.clear();
  }

  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
}

/** Every observer watching an element in els sees it intersect. */
export function bringIntoView(...els: Element[]) {
  for (const observer of FakeIntersectionObserver.all) {
    const entries = els
      .filter((el) => observer.observed.has(el))
      .map((target) => ({ target, isIntersecting: true }) as IntersectionObserverEntry);
    if (entries.length) {
      observer.callback(entries, observer as unknown as IntersectionObserver);
    }
  }
}

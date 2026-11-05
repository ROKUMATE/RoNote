import { expect, test } from "vitest";
import { type RestoreEnv, restoreAnchor, topAnchor } from "./scrollAnchor";

/**
 * A page with one block, a scroll position and the events restoring
 * listens to, all driven by the test.
 */
function fakePage({ blockTop, drawn = true }: { blockTop: number; drawn?: boolean }) {
  const page = {
    scrollY: 0,
    blockTop,
    drawn,
    scrolls: [] as number[],
    layout: undefined as (() => void) | undefined,
    userScroll: undefined as (() => void) | undefined,
    timer: undefined as (() => void) | undefined,
  };
  const env: RestoreEnv = {
    top: () => (page.drawn ? page.blockTop - page.scrollY : undefined),
    scrollBy: (dy) => {
      page.scrolls.push(dy);
      page.scrollY += dy;
    },
    onLayout: (changed) => {
      page.layout = changed;
      return () => {
        page.layout = undefined;
      };
    },
    onUserScroll: (scrolled) => {
      page.userScroll = scrolled;
      return () => {
        page.userScroll = undefined;
      };
    },
    after: (_ms, run) => {
      page.timer = run;
      return () => {
        page.timer = undefined;
      };
    },
  };
  /** Something above the block grows by dy, and the page lays out again. */
  const grow = (dy: number) => {
    page.blockTop += dy;
    page.layout?.();
  };
  return { page, env, grow };
}

const box = (top: number, height: number) => ({
  getBoundingClientRect: () => ({ top, bottom: top + height }) as DOMRect,
});

test("the anchor is the first block still showing at the top of the viewport", () => {
  const blocks = [
    { blockId: "a", element: box(-300, 200) as Element },
    { blockId: "b", element: box(-100, 150) as Element },
    { blockId: "c", element: box(50, 100) as Element },
  ];
  expect(topAnchor(blocks)).toEqual({ blockId: "b", offset: -100 });
  expect(topAnchor([])).toBeUndefined();
});

test("coming back puts the block where it was", () => {
  const { page, env } = fakePage({ blockTop: 3500 });

  restoreAnchor({ blockId: "b", offset: 40 }, env);

  expect(page.scrolls).toEqual([3460]);
  expect(page.blockTop - page.scrollY).toBe(40);
});

test("a block not drawn yet is put back once it is", () => {
  const { page, env } = fakePage({ blockTop: 3500, drawn: false });
  restoreAnchor({ blockId: "b", offset: 40 }, env);
  expect(page.scrolls).toEqual([]);

  page.drawn = true;
  page.layout?.();

  expect(page.blockTop - page.scrollY).toBe(40);
});

test("a change above the block in the first second is put right once, then restoring stops", () => {
  const { page, env, grow } = fakePage({ blockTop: 3500 });
  restoreAnchor({ blockId: "b", offset: 40 }, env);

  grow(31.5);
  expect(page.blockTop - page.scrollY).toBe(40);
  expect(page.layout).toBeUndefined();

  // Any later change is the page's to make: nothing listens any more.
  grow(20);
  expect(page.scrolls).toHaveLength(2);
});

test("a layout change that leaves the block where it was changes nothing", () => {
  const { page, env } = fakePage({ blockTop: 3500 });
  restoreAnchor({ blockId: "b", offset: 40 }, env);

  page.layout?.();

  expect(page.scrolls).toHaveLength(1);
  expect(page.layout).toBeDefined();
});

test("the reader scrolling during the restore stops it: their scrolling wins", () => {
  const { page, env, grow } = fakePage({ blockTop: 3500, drawn: false });
  restoreAnchor({ blockId: "b", offset: 40 }, env);

  page.userScroll?.();
  page.drawn = true;
  grow(31.5);

  expect(page.scrolls).toEqual([]);
  expect([page.layout, page.userScroll, page.timer]).toEqual([undefined, undefined, undefined]);
});

test("after the first second, restoring has stopped", () => {
  const { page, env, grow } = fakePage({ blockTop: 3500 });
  restoreAnchor({ blockId: "b", offset: 40 }, env);

  page.timer?.();
  grow(31.5);

  expect(page.scrolls).toHaveLength(1);
});

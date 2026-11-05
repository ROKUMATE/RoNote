import type { DocContent, IrBlock, TextSelector } from "../api/schema";
import { eachBlock } from "./walk";

/*
 * Anchoring (docs/adr/0007-annotations-and-anchoring.md): where a
 * highlight's selector lands in a block's current text. Offsets count
 * UTF-16 code units, as strings do here.
 */

/** A range of a block's plain text, end exclusive. */
export type Range = { start: number; end: number };

/** How much text before and after a quote a selector keeps. */
const contextLength = 32;

/** The least similarity, 1 − edit distance / quote length, a fuzzy match needs. */
const fuzzyThreshold = 0.75;

/**
 * The plain text highlights count in: a heading's or paragraph's runs
 * joined, or a code block's text. Undefined for blocks that take none.
 */
export function blockText(block: IrBlock): string | undefined {
  switch (block.kind) {
    case "heading":
    case "paragraph":
      return (block.content ?? []).map((run) => run.text).join("");
    case "code":
      return block.text ?? "";
    default:
      return undefined;
  }
}

/** The plain text of block id, at any depth of content. */
export function textOf(content: DocContent, id: string): string | undefined {
  let text: string | undefined;
  eachBlock(content.blocks, (block) => {
    if (block.id === id && text === undefined) text = blockText(block);
  });
  return text;
}

/** The selector of text's range start to end. */
export function selectorOf(text: string, start: number, end: number): TextSelector {
  return {
    position: { start, end },
    quote: {
      exact: text.slice(start, end),
      prefix: text.slice(Math.max(0, start - contextLength), start),
      suffix: text.slice(end, end + contextLength),
    },
  };
}

/**
 * Where selector lands in text: at its position if the quote is still
 * there; else at the occurrence of the quote whose surroundings agree most
 * (then the nearest); else at the closest fuzzy match, if similar enough.
 * Undefined means the highlight is orphaned.
 */
export function anchor(selector: TextSelector, text: string): Range | undefined {
  const { start, end } = selector.position;
  const { exact } = selector.quote;
  if (text.slice(start, end) === exact) return { start, end };
  const hits: Range[] = [];
  for (let at = text.indexOf(exact); at >= 0; at = text.indexOf(exact, at + 1)) {
    hits.push({ start: at, end: at + exact.length });
  }
  return closest(hits.length > 0 ? hits : fuzzyMatches(text, exact), selector, text);
}

/** selector moved to where it lands in text, with its quote taken afresh. */
export function reanchor(selector: TextSelector, text: string): TextSelector | undefined {
  const range = anchor(selector, text);
  return range && selectorOf(text, range.start, range.end);
}

/** Whether two selectors name the same range with the same text. */
export function sameSelector(a: TextSelector, b: TextSelector): boolean {
  return (
    a.position.start === b.position.start &&
    a.position.end === b.position.end &&
    a.quote.exact === b.quote.exact &&
    a.quote.prefix === b.quote.prefix &&
    a.quote.suffix === b.quote.suffix
  );
}

/** Of ranges, the one whose surroundings agree most with selector's, then the nearest. */
function closest(ranges: Range[], selector: TextSelector, text: string): Range | undefined {
  const { prefix, suffix } = selector.quote;
  const score = (r: Range) =>
    sharedSuffix(text.slice(0, r.start), prefix) + sharedPrefix(text.slice(r.end), suffix);
  const distance = (r: Range) => Math.abs(r.start - selector.position.start);
  let best: Range | undefined;
  for (const r of ranges) {
    if (
      !best ||
      score(r) > score(best) ||
      (score(r) === score(best) && distance(r) < distance(best))
    ) {
      best = r;
    }
  }
  return best;
}

function sharedPrefix(a: string, b: string): number {
  let n = 0;
  while (n < a.length && n < b.length && a[n] === b[n]) n++;
  return n;
}

function sharedSuffix(a: string, b: string): number {
  let n = 0;
  while (n < a.length && n < b.length && a[a.length - 1 - n] === b[b.length - 1 - n]) n++;
  return n;
}

/**
 * The substrings of text nearest to pattern by edit distance, if within
 * fuzzyThreshold: Sellers' algorithm (any start in text is free), with
 * Ukkonen's cutoff so only rows that can still match are computed.
 */
function fuzzyMatches(text: string, pattern: string): Range[] {
  const m = pattern.length;
  const k = Math.floor(m * (1 - fuzzyThreshold));
  if (m === 0 || k === 0) return [];
  const over = k + 1;
  const codes = Int32Array.from(pattern, (_, i) => pattern.charCodeAt(i));
  // Column j holds, for each pattern prefix length i, the least distance of
  // a substring of text ending at j, and where that substring starts. Rows
  // past top are over k, so never read.
  let dist = Int32Array.from({ length: m + 1 }, (_, i) => i);
  let from = new Int32Array(m + 1);
  let next = new Int32Array(m + 1);
  let nextFrom = new Int32Array(m + 1);
  let top = Math.min(k, m);
  let best = over;
  let matches: Range[] = [];
  for (let j = 1; j <= text.length; j++) {
    const c = text.charCodeAt(j - 1);
    next[0] = 0;
    nextFrom[0] = j;
    const rows = Math.min(top + 1, m);
    // Typed arrays never read undefined within their length: the casts
    // only tell the compiler so, and keep this loop free of checks.
    for (let i = 1; i <= rows; i++) {
      let d = (dist[i - 1] as number) + (codes[i - 1] === c ? 0 : 1);
      let f = from[i - 1] as number;
      if (i <= top && (dist[i] as number) + 1 < d) {
        d = (dist[i] as number) + 1;
        f = from[i] as number;
      }
      if ((next[i - 1] as number) + 1 < d) {
        d = (next[i - 1] as number) + 1;
        f = nextFrom[i - 1] as number;
      }
      next[i] = Math.min(d, over);
      nextFrom[i] = f;
    }
    top = rows;
    while (top > 0 && (next[top] as number) > k) top--;
    if (top === m) {
      const d = next[m] as number;
      const range = { start: nextFrom[m] as number, end: j };
      if (d < best) {
        best = d;
        matches = [range];
      } else if (d === best) {
        matches.push(range);
      }
    }
    [dist, next] = [next, dist];
    [from, nextFrom] = [nextFrom, from];
  }
  return matches;
}

import { expect, test } from "vitest";
import fixtureText from "../../../../testdata/anchoring/blocktext.json?raw";
import type { DocContent } from "../api/schema";
import { anchor, blockText, reanchor, sameSelector, selectorOf, textOf } from "./anchoring";

const sentence = "The quick brown fox jumps over the lazy dog.";

/** The selector of the first occurrence of quote in text. */
function select(text: string, quote: string) {
  const start = text.indexOf(quote);
  if (start < 0) throw new Error(`${quote} is not in the text`);
  return selectorOf(text, start, start + quote.length);
}

/** The text a range covers. */
function at(text: string, selector = select(sentence, "quick")) {
  const range = anchor(selector, text);
  return range && text.slice(range.start, range.end);
}

test("a selector keeps the text around its quote", () => {
  expect(select(sentence, "fox")).toEqual({
    position: { start: 16, end: 19 },
    quote: { exact: "fox", prefix: "The quick brown ", suffix: " jumps over the lazy dog." },
  });
  const long = `${"a".repeat(40)}X${"b".repeat(40)}`;
  const { quote } = select(long, "X");
  expect([quote.prefix.length, quote.suffix.length]).toEqual([32, 32]);
});

test("unchanged text anchors at the position", () => {
  expect(anchor(select(sentence, "quick"), sentence)).toEqual({ start: 4, end: 9 });
});

test("text inserted before the highlight: the stale position gives way to the quote", () => {
  const edited = `Indeed, ${sentence}`;
  expect(anchor(select(sentence, "quick"), edited)).toEqual({ start: 12, end: 17 });
});

test("a repeated quote goes to the occurrence whose surroundings agree", () => {
  const text = "Use go build to compile. Then go build again to check.";
  const second = selectorOf(text, 30, 38);
  // Text inserted before both occurrences moves them; the position now
  // falls on neither.
  const edited = `Note: ${text}`;
  expect(anchor(second, edited)).toEqual({ start: 36, end: 44 });
  // Context wins over distance: an occurrence added nearer the old
  // position loses to the one whose surroundings are the highlight's.
  const original = "Run go build first. Later, run go build with -race.";
  const later = selectorOf(original, 31, 39);
  const added = "Run go build first, then go build again. Later, run go build with -race.";
  const third = added.lastIndexOf("go build");
  expect(anchor(later, added)).toEqual({ start: third, end: third + 8 });
});

test("a word edited inside the quote still anchors, above the threshold", () => {
  const selector = select(sentence, "The quick brown fox jumps");
  const edited = "The fast brown fox jumps over the lazy dog.";
  expect(at(edited, selector)).toBe("The fast brown fox jumps");
  expect(reanchor(selector, edited)?.quote.exact).toBe("The fast brown fox jumps");
});

test("the fuzzy threshold: a quarter of the quote's characters changed anchors, more orphans", () => {
  // 20 characters: 5 changed is 0.75 similar, 6 changed is 0.7.
  const text = "before 0123456789abcdefghij after";
  const selector = select(text, "0123456789abcdefghij");
  expect(at("before 01234XXXXXabcdefghij after", selector)).toBe("01234XXXXXabcdefghij");
  expect(at("before 0123XXXXXXabcdefghij after", selector)).toBeUndefined();
});

test("a short quote takes a typo, but not a replaced word", () => {
  expect(at("The quack brown fox", select(sentence, "quick"))).toBe("quack");
  expect(at("The fast brown fox", select(sentence, "quick"))).toBeUndefined();
  expect(at("The fox", select(sentence, "fox"))).toBe("fox");
  expect(at("The cat", select(sentence, "fox"))).toBeUndefined();
});

test("deleted words orphan the highlight", () => {
  expect(at("The brown fox jumps over the lazy dog.", select(sentence, "quick"))).toBeUndefined();
  expect(at("", select(sentence, "quick"))).toBeUndefined();
});

test("revert to source brings the highlight back to its words", () => {
  const original = select(sentence, "lazy dog");
  const edited = `Look: ${sentence.replace("jumps", "leaps high")}`;
  const moved = reanchor(original, edited);
  expect(moved && edited.slice(moved.position.start, moved.position.end)).toBe("lazy dog");

  const back = moved && reanchor(moved, sentence);

  expect(back && sameSelector(back, original)).toBe(true);
});

test("code: offsets count tabs and line breaks, and quotes span lines", () => {
  const code = 'func main() {\n\tfmt.Println("hi")\n}\n';
  const call = select(code, "fmt.Println");
  expect(call.position).toEqual({ start: 15, end: 26 });
  const lines = select(code, '{\n\tfmt.Println("hi")');
  const edited = `package main\n\n${code}`;
  expect(at(edited, call)).toBe("fmt.Println");
  expect(anchor(lines, edited)).toEqual({ start: 26, end: 46 });
});

test("offsets count UTF-16 code units, so an emoji counts two", () => {
  const text = "Ship it 🚀 now, naïvely.";
  const selector = select(text, "🚀 now");
  expect(selector.position).toEqual({ start: 8, end: 14 });
  expect(at(`Then ${text}`, selector)).toBe("🚀 now");
});

test("block text matches the server's, from the shared fixture", () => {
  const fixture: {
    content: DocContent;
    texts: Record<string, { text: string; length: number }>;
    none: string[];
  } = JSON.parse(fixtureText);
  for (const [id, want] of Object.entries(fixture.texts)) {
    expect(textOf(fixture.content, id)).toBe(want.text);
    expect(want.text.length).toBe(want.length);
  }
  for (const id of fixture.none) {
    expect(textOf(fixture.content, id)).toBeUndefined();
  }
  expect(blockText({ id: "x", kind: "divider" })).toBeUndefined();
});

test("a long quote edited throughout still anchors in a long block", () => {
  let seed = 7;
  const words = Array.from({ length: 2500 }, () => {
    seed = (Math.imul(seed, 1103515245) + 12345) >>> 0;
    return ["go", "build", "the", "module", "cache", "path", "test", "run"][(seed >>> 16) % 8];
  });
  const text = words.join(" ").slice(0, 10_000);
  const selector = selectorOf(text, 5000, 7000);
  // Every 25th character changed: 4% of the quote, and the position stale.
  const edited = `x${[...text].map((c, i) => (i >= 5000 && i < 7000 && i % 25 === 0 ? "#" : c)).join("")}`;

  const range = anchor(selector, edited);

  expect(range && Math.abs(range.start - 5001)).toBeLessThan(5);
  expect(range && Math.abs(range.end - 7001)).toBeLessThan(5);
});

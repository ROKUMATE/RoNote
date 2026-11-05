import type { Range } from "./anchoring";
import type { Placements } from "./highlights";

/*
 * Highlights in code (docs/adr/0007-annotations-and-anchoring.md): the
 * code view cuts its runs of text, highlighted or plain, at the edges of
 * the highlights and wraps the pieces inside one. The text itself never
 * changes: the pieces join back to it exactly.
 */

/** A highlight in a code block's text. */
export type CodeMark = Range & { id: string };

/** A run of code text, with its colour when highlighted, and the highlight it is in. */
export type Run = { text: string; color?: string | undefined; mark?: string | undefined };

/** runs cut at the edges of marks, each piece inside a mark carrying its id. */
export function cutRuns(runs: readonly Run[], marks: readonly CodeMark[]): Run[] {
  if (marks.length === 0) return [...runs];
  const edges = [...new Set(marks.flatMap((m) => [m.start, m.end]))].sort((a, b) => a - b);
  const out: Run[] = [];
  let at = 0;
  for (const run of runs) {
    let start = 0;
    while (start < run.text.length) {
      const pos = at + start;
      const next = edges.find((e) => e > pos) ?? Number.POSITIVE_INFINITY;
      const end = Math.min(run.text.length, next - at);
      const mark = marks.find((m) => m.start <= pos && pos < m.end)?.id;
      out.push({ text: run.text.slice(start, end), color: run.color, mark });
      start = end;
    }
    at += run.text.length;
  }
  return out;
}

/** The placed highlights of every block, by block id. */
export function marksByBlock(placements: Placements): Map<string, CodeMark[]> {
  const byBlock = new Map<string, CodeMark[]>();
  for (const h of placements.highlights) {
    const range = placements.placed.get(h.id);
    if (!range) continue;
    const marks = byBlock.get(h.block_id) ?? [];
    marks.push({ id: h.id, ...range });
    byBlock.set(h.block_id, marks);
  }
  return byBlock;
}

/** Whether two maps hold the same marks, so views need not draw again. */
export function sameMarks(
  a: ReadonlyMap<string, readonly CodeMark[]>,
  b: ReadonlyMap<string, readonly CodeMark[]>,
): boolean {
  if (a.size !== b.size) return false;
  for (const [block, marks] of a) {
    const other = b.get(block);
    if (
      !other ||
      other.length !== marks.length ||
      marks.some(
        (m, i) => m.id !== other[i]?.id || m.start !== other[i]?.start || m.end !== other[i]?.end,
      )
    ) {
      return false;
    }
  }
  return true;
}

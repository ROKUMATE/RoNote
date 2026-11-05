/** A note to place: where its highlight is, and how tall the note is. */
export type Placed = { id: string; top: number; height: number };

/**
 * Where each note goes: level with its highlight, unless the note above
 * reaches that far, in which case just below it. Notes keep the order of
 * their highlights.
 */
export function stack(notes: readonly Placed[], gap: number): Map<string, number> {
  const tops = new Map<string, number>();
  let free = Number.NEGATIVE_INFINITY;
  for (const note of [...notes].sort((a, b) => a.top - b.top)) {
    const top = Math.max(note.top, free);
    tops.set(note.id, top);
    free = top + note.height + gap;
  }
  return tops;
}

import type { RunnerEvent } from "../../runner/protocol";
import { pythonVersion } from "./python";

/*
 * What a check of a >>> transcript found (docs/adr/0008-in-browser-code-execution.md):
 * each example as doctest judged it, with the differences that aren't the
 * reader's mistake named as such.
 */

/** One example as the runner checked it. */
export type Example = Pick<
  Extract<RunnerEvent, { type: "example" }>,
  "source" | "want" | "got" | "outcome"
>;

/** A failure that isn't the reader's: the output differs only where it always would. */
export type Known = "address" | "version";

export const knownReasons: Record<Known, string> = {
  address: "Only an object's address differs, and addresses change on every run.",
  version: `Only the error's wording differs, which changes between Python versions; this browser runs Python ${pythonVersion}.`,
};

/**
 * An object's address where a repr ends (`at 0x7f3a2c1e5d90>`), its 0x left
 * out as some pages print it (`at 10042ed0>`). Other hex, such as hex(255)'s,
 * is output like any other.
 */
const address = /(?<=\bat )(?:0x)?[0-9a-f]+(?=>)/gi;
const header = "Traceback (most recent call last):";

/** The exception a traceback ends with: its type's last name, and its message. */
function exception(output: string): { type: string; message: string } | undefined {
  if (!output.includes(header)) return undefined;
  const last = output.trimEnd().split("\n").at(-1) ?? "";
  const match = /^([A-Za-z_][\w.]*)(?::\s*(.*))?$/.exec(last);
  if (!match?.[1]) return undefined;
  return { type: match[1].split(".").at(-1) ?? match[1], message: match[2] ?? "" };
}

/** Why a failed example isn't the reader's mistake, if it isn't. */
export function known(example: Example): Known | undefined {
  if (example.outcome === "passed") return undefined;
  const { want, got } = example;
  if (want.replace(address, "@") === got.replace(address, "@")) return "address";
  const expected = exception(want);
  const raised = exception(got);
  if (expected && raised && expected.type === raised.type) return "version";
  return undefined;
}

export type DiffLine = { kind: "same" | "want" | "got"; text: string };

function lines(text: string): string[] {
  return text === "" ? [] : text.replace(/\n$/, "").split("\n");
}

/** The lines of want and got, those only in one marked, in order (longest common subsequence). */
export function diff(want: string, got: string): DiffLine[] {
  const a = lines(want);
  const b = lines(got);
  // common[i][j]: the longest common subsequence of a from i and b from j.
  const common = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      const row = common[i] as number[];
      row[j] =
        a[i] === b[j]
          ? (common[i + 1]?.[j + 1] ?? 0) + 1
          : Math.max(common[i + 1]?.[j] ?? 0, row[j + 1] ?? 0);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      out.push({ kind: "same", text: a[i] ?? "" });
      i++;
      j++;
    } else if (
      i < a.length &&
      (j === b.length || (common[i + 1]?.[j] ?? 0) >= (common[i]?.[j + 1] ?? 0))
    ) {
      // On a tie the expected line goes first, as in a unified diff.
      out.push({ kind: "want", text: a[i] ?? "" });
      i++;
    } else {
      out.push({ kind: "got", text: b[j] ?? "" });
      j++;
    }
  }
  return out;
}

/** An example's source as the page shows it: >>> first, ... after. */
export function prompt(source: string): string {
  return lines(source)
    .map((line, i) => `${i === 0 ? ">>>" : "..."} ${line}`)
    .join("\n");
}

/** How a check went, in one line. */
export function summary(examples: readonly Example[]): string {
  const total = examples.length;
  if (total === 0) return "Found no examples to check";
  const passed = examples.filter((e) => e.outcome === "passed").length;
  const explained = examples.filter((e) => known(e)).length;
  const failed = total - passed - explained;
  const noun = total === 1 ? "example" : "examples";
  if (passed === total) return total === 1 ? "The example passed" : `All ${total} examples passed`;
  return [
    `${passed} of ${total} ${noun} passed`,
    ...(failed > 0 ? [`${failed} failed`] : []),
    ...(explained > 0 ? [`${explained} with a known difference`] : []),
  ].join(", ");
}

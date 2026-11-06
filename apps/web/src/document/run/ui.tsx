import { ArrowCounterClockwiseIcon } from "@phosphor-icons/react/ArrowCounterClockwise";
import { ListChecksIcon } from "@phosphor-icons/react/ListChecks";
import { PlayIcon } from "@phosphor-icons/react/Play";
import { StopIcon } from "@phosphor-icons/react/Stop";
import { useContext, useEffect, useId, useState, useSyncExternalStore } from "react";
import type { IrBlock } from "../../api/schema";
import { outputLimit, runLimit } from "../../runner/limits";
import type { Runtime } from "../../runner/protocol";
import { type ChosenLangs, CodeLangsContext } from "../codeLangs";
import { classifyAll, codeBlocks, isDoctest, type RunClass, type Runnability } from "./classify";
import { diff, type Example, known, knownReasons, prompt, summary } from "./doctest";
import { onRunKey, runShortcut, runShortcutLabel } from "./keys";
import type { RunScope } from "./RunArea";
import { type BlockRun, type Chunk, runsOf, runtimeNames, type Session } from "./runs";

/*
 * Run controls and output for code blocks, a lazy chunk
 * (docs/adr/0008-in-browser-code-execution.md).
 */

/** What a block's chip says; its reason shows on hover and focus. */
const chips: Record<RunClass, string> = {
  browser_python: "Python · browser",
  python_doctest: "Python · browser",
  browser_js: "JavaScript · browser",
  needs_packages: "Needs packages",
  needs_network: "Needs network",
  needs_secret: "Needs a key",
  needs_input: "Needs input",
  needs_dom: "Needs a display",
  shell: "Shell",
  fragment: "Fragment",
  unlabelled: "No language",
  unsupported: "Can't run",
};

/** The last document's classes: every block of a document classified at once. */
let classified:
  | { blocks: readonly IrBlock[]; chosen: ChosenLangs; classes: Map<string, Runnability> }
  | undefined;

function classesOf(blocks: readonly IrBlock[], chosen: ChosenLangs) {
  if (classified?.blocks !== blocks || classified.chosen !== chosen) {
    classified = { blocks, chosen, classes: classifyAll(codeBlocks(blocks, chosen)) };
  }
  return classified.classes;
}

function useRun(scope: RunScope, blockId: string) {
  const runs = runsOf(scope.documentId);
  const run = useSyncExternalStore(runs.subscribe, () => runs.get(blockId));
  const session = useSyncExternalStore(runs.subscribe, () => {
    const last = runs.get(blockId);
    return last ? runs.session(last.runtime) : "off";
  });
  return { runs, run, session };
}

/**
 * A block's chip, and Run, Run anyway or Stop; a transcript can also check
 * its examples. Mod+Enter does what Run does, while the block is focused.
 */
export function Controls({ block, scope }: { block: IrBlock; scope: RunScope }) {
  const { chosen } = useContext(CodeLangsContext);
  const runnability = classesOf(scope.blocks, chosen).get(block.id);
  const { runs, run } = useRun(scope, block.id);
  const code = block.text ?? "";
  const runtime = runnability?.run === "none" ? undefined : runnability?.runtime;
  const mode = runtime === "python" && isDoctest(code) ? "doctest" : "exec";
  useEffect(() => {
    if (!runtime) return;
    return onRunKey(block.id, () => {
      const last = runs.get(block.id);
      if (!last || last.done) runs.run(block.id, runtime, code, mode);
    });
  }, [block.id, runs, runtime, code, mode]);
  if (!runnability) return null;
  const buttons =
    run && !run.done ? (
      <button type="button" className="run-button" onClick={() => runs.stop(run.runtime)}>
        <StopIcon size={16} aria-hidden />
        Stop
      </button>
    ) : !runtime ? null : (
      <>
        <button
          type="button"
          className="run-button"
          aria-keyshortcuts={runShortcut}
          title={runShortcutLabel}
          onClick={() => runs.run(block.id, runtime, code, mode)}
        >
          <PlayIcon size={16} aria-hidden />
          {runnability.run === "run" ? "Run" : "Run anyway"}
        </button>
        {runnability.class === "python_doctest" && (
          <button
            type="button"
            className="run-button"
            onClick={() => runs.run(block.id, runtime, code, "check")}
          >
            <ListChecksIcon size={16} aria-hidden />
            Check examples
          </button>
        )}
      </>
    );
  return (
    <>
      <Chip runnability={runnability} />
      {buttons}
    </>
  );
}

/**
 * The class as a chip, its reason a tooltip that Escape dismisses. A block
 * with no language opens its own language list from the chip, so the choice
 * its reason asks for is right there.
 */
function Chip({ runnability }: { runnability: Runnability }) {
  const id = useId();
  const [dismissed, setDismissed] = useState(false);
  const chooses = runnability.class === "unlabelled";
  return (
    <span className="run-chip-wrap" data-dismissed={dismissed || undefined}>
      <button
        type="button"
        className="run-chip"
        data-run={runnability.run}
        aria-describedby={id}
        aria-haspopup={chooses ? "listbox" : undefined}
        onClick={
          chooses
            ? (e) =>
                e.currentTarget
                  .closest(".code-block")
                  ?.querySelector<HTMLButtonElement>(".code-lang")
                  ?.click()
            : undefined
        }
        onKeyDown={(e) => {
          if (e.key === "Escape") setDismissed(true);
        }}
        onBlur={() => setDismissed(false)}
        onMouseLeave={() => setDismissed(false)}
      >
        {chips[runnability.class]}
      </button>
      <span role="tooltip" id={id} className="run-tip">
        {runnability.reason}
      </span>
    </span>
  );
}

const labels: Record<Exclude<Chunk["kind"], "stdout">, string> = {
  stderr: "stderr",
  warn: "warning",
  error: "error",
  result: "value",
};

const runtimes: readonly Runtime[] = ["python", "javascript"];

/**
 * Each runtime the document has blocks to run in (with Run or Run anyway):
 * where its session stands, and its Restart. For the document's header.
 */
export function Sessions({ scope }: { scope: RunScope }) {
  const { chosen } = useContext(CodeLangsContext);
  const classes = [...classesOf(scope.blocks, chosen).values()];
  const present = runtimes.filter((r) => classes.some((c) => c.run !== "none" && c.runtime === r));
  if (present.length === 0) return null;
  return (
    <ul
      aria-label="Code sessions"
      className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-ink-2 text-ui"
    >
      {present.map((runtime) => (
        <SessionStatus key={runtime} scope={scope} runtime={runtime} />
      ))}
    </ul>
  );
}

function SessionStatus({ scope, runtime }: { scope: RunScope; runtime: Runtime }) {
  const runs = runsOf(scope.documentId);
  const status = useSyncExternalStore(runs.subscribe, () => runs.status(runtime));
  const name = runtimeNames[runtime];
  return (
    <li className="flex items-center gap-2">
      <span>
        {name}: {status}
      </span>
      <button
        type="button"
        aria-label={`Restart ${name}`}
        disabled={status === "not started"}
        onClick={() => runs.restart(runtime)}
        className="inline-flex items-center gap-1 rounded-sm px-2 py-0.5 hover:enabled:bg-paper-2 hover:enabled:text-ink disabled:opacity-60"
      >
        <ArrowCounterClockwiseIcon size={16} aria-hidden />
        Restart
      </button>
    </li>
  );
}

/** A block's output once it has run: never saved, gone with the page. */
export function Output({ block, scope }: { block: IrBlock; scope: RunScope }) {
  const { runs, run, session } = useRun(scope, block.id);
  if (!run) return null;
  return (
    <div className="run-output">
      <div className="run-output-bar">
        <p role="status">{status(run, session)}</p>
        <button type="button" disabled={!run.done} onClick={() => runs.clear(block.id)}>
          Clear
        </button>
        <button type="button" onClick={() => runs.restart(run.runtime, block.id)}>
          <ArrowCounterClockwiseIcon size={16} aria-hidden />
          Restart
        </button>
      </div>
      {run.note && <p className="run-note">{run.note}</p>}
      {run.examples && run.examples.length > 0 && <Examples examples={run.examples} />}
      {run.output.length > 0 && (
        // biome-ignore lint/a11y/noNoninteractiveTabindex: output that scrolls must be reachable by keyboard (WCAG 2.1.1; axe scrollable-region-focusable).
        <pre tabIndex={0} className="run-text">
          {run.output.map((chunk, i) =>
            chunk.kind === "stdout" ? (
              // biome-ignore lint/suspicious/noArrayIndexKey: output only grows at its end.
              <span key={i}>{chunk.text}</span>
            ) : (
              // biome-ignore lint/suspicious/noArrayIndexKey: output only grows at its end.
              <span key={i} className={`run-${chunk.kind}`}>
                <span className="run-label">{labels[chunk.kind]}</span>
                {chunk.text}
              </span>
            ),
          )}
        </pre>
      )}
    </div>
  );
}

const verdicts: Record<Example["outcome"], string> = {
  passed: "Passed",
  failed: "Failed",
  error: "Failed",
};

/** A check's examples, each with its verdict, and a failure's diff. */
function Examples({ examples }: { examples: readonly Example[] }) {
  return (
    <ol aria-label="Examples" className="run-examples">
      {examples.map((example, i) => {
        const why = known(example);
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: examples only grow at their end.
          <li key={i} data-outcome={why ? "known" : example.outcome}>
            <p className="run-example">
              <span className="run-verdict">
                {why ? "Known difference" : verdicts[example.outcome]}
              </span>
              <code>{prompt(example.source)}</code>
            </p>
            {why && <p className="run-known">{knownReasons[why]}</p>}
            {example.outcome !== "passed" && <Diff want={example.want} got={example.got} />}
          </li>
        );
      })}
    </ol>
  );
}

const marks = { same: " ", want: "-", got: "+" } as const;

/** What the page expected against what the example gave, line by line. */
function Diff({ want, got }: { want: string; got: string }) {
  return (
    <>
      <p className="run-diff-legend">- expected, + got</p>
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: a diff that scrolls must be reachable by keyboard (WCAG 2.1.1; axe scrollable-region-focusable). */}
      <pre tabIndex={0} className="run-diff">
        {diff(want, got).map((line, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: a diff's lines are positional.
          <span key={i} data-kind={line.kind}>
            <span className="run-diff-mark">{marks[line.kind]} </span>
            {line.text}
            {"\n"}
          </span>
        ))}
      </pre>
    </>
  );
}

function status(run: BlockRun, session: Session): string {
  if (!run.done) return session === "ready" ? "Running…" : `Starting ${runtimeNames[run.runtime]}…`;
  const after = duration(run.done.ms);
  switch (run.done.end) {
    case "ok":
      return run.examples ? `${summary(run.examples)}, in ${after}` : `Ran in ${after}`;
    case "error":
      return `Ended with an error after ${after}`;
    case "stop":
      return `Stopped after ${after}`;
    case "timeout":
      return `Stopped at the ${runLimit / 1000} second limit`;
    case "output":
      return `Stopped: the output passed ${outputLimit / 1_000_000} MB`;
    case "lost":
      return "Didn't finish";
  }
}

function duration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}

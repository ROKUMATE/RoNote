import {
  createContext,
  lazy,
  type ReactNode,
  Suspense,
  useContext,
  useEffect,
  useMemo,
} from "react";
import type { IrBlock } from "../../api/schema";

/*
 * Running code blocks (docs/adr/0008-in-browser-code-execution.md). The
 * classifier, controls, output and runner client are a chunk of their own,
 * loaded with a document's first code block; this is all the document chunk
 * carries.
 */

/** The open document whose code blocks run, and its blocks. */
export type RunScope = { documentId: string; blocks: readonly IrBlock[] };

const RunScopeContext = createContext<RunScope | undefined>(undefined);

/** What ends each open document's session, set by the run chunk. */
const enders = new Map<string, () => void>();

/** Sets how documentId's session ends when its page closes. */
export function onClose(documentId: string, end: () => void) {
  enders.set(documentId, end);
}

/** Lets the code blocks below run, in one session that ends when this does. */
export function RunScopeProvider({
  documentId,
  blocks,
  children,
}: RunScope & { children: ReactNode }) {
  useEffect(
    () => () => {
      enders.get(documentId)?.();
      enders.delete(documentId);
    },
    [documentId],
  );
  const scope = useMemo(() => ({ documentId, blocks }), [documentId, blocks]);
  return <RunScopeContext value={scope}>{children}</RunScopeContext>;
}

const Controls = lazy(() => import("./ui").then((m) => ({ default: m.Controls })));
const Output = lazy(() => import("./ui").then((m) => ({ default: m.Output })));

/** A code block's run controls, for its header; nothing outside a document. */
export function RunControls({ block }: { block: IrBlock }) {
  const scope = useContext(RunScopeContext);
  return scope ? (
    <Suspense fallback={null}>
      <Controls block={block} scope={scope} />
    </Suspense>
  ) : null;
}

/** A code block's output, once it has run. */
export function RunOutput({ block }: { block: IrBlock }) {
  const scope = useContext(RunScopeContext);
  return scope ? (
    <Suspense fallback={null}>
      <Output block={block} scope={scope} />
    </Suspense>
  ) : null;
}

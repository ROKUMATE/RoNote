import { CheckCircleIcon } from "@phosphor-icons/react/CheckCircle";
import { CircleIcon } from "@phosphor-icons/react/Circle";
import { CircleHalfIcon } from "@phosphor-icons/react/CircleHalf";
import { FilePlusIcon } from "@phosphor-icons/react/FilePlus";
import { XIcon } from "@phosphor-icons/react/X";
import * as Tabs from "@radix-ui/react-tabs";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  type DragEvent,
  type FormEvent,
  type RefObject,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { requestErrorMessage, requestErrorMessages } from "../api/messages";
import { type CreatedImport, createImport, maxPdfBytes, uploadPdf } from "../api/queries";
import type { ErrorCode } from "../api/schema";
import { buttonClass } from "../ui/button";
import { endingMessage, stepLabels } from "./describe";
import { stepState, stepsOf, type TrackedImport } from "./state";
import { useImportTracker, useTrackedImports } from "./tracker";

/** CreateImportRequest.url's maxLength in api/openapi.yaml. */
export const maxUrlLength = 2048;

/**
 * Checks a URL the way the API will, so most mistakes show at once. The API
 * stays the authority: it also refuses what this lets through.
 */
function urlProblem(raw: string): ErrorCode | undefined {
  const url = raw.trim();
  if (url.length > maxUrlLength) return "url_too_long";
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return "invalid_url";
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "unsupported_scheme";
  return parsed.hostname ? undefined : "invalid_url";
}

/**
 * Checks a file the way the API will: a PDF, by its type or its name, of at
 * most 25 MB. The API stays the authority: it reads the file's first bytes.
 */
function pdfProblem(file: File): ErrorCode | undefined {
  if (file.type !== "application/pdf" && !file.name.toLowerCase().endsWith(".pdf")) {
    return "not_a_pdf";
  }
  return file.size > maxPdfBytes ? "pdf_too_large" : undefined;
}

/** The "Import page" button and the dialog it opens. */
export function ImportButton() {
  const dialog = useRef<HTMLDialogElement>(null);
  return (
    <>
      <button type="button" onClick={() => dialog.current?.showModal()} className={buttonClass}>
        <FilePlusIcon size={16} aria-hidden />
        Import page
      </button>
      <ImportDialog dialog={dialog} />
    </>
  );
}

const tabClass =
  "rounded-sm px-3 py-1 text-ui-lg text-ink-2 hover:text-ink aria-selected:bg-paper-2 aria-selected:text-ink";

/**
 * Asks for a URL, or a PDF to upload, and follows the import. Closing it
 * never cancels: the tracker keeps following, and the library shows the
 * import until it ends.
 */
function ImportDialog({ dialog }: { dialog: RefObject<HTMLDialogElement | null> }) {
  const titleId = useId();
  const inputId = useId();
  const fileId = useId();
  const problemId = useId();
  const navigate = useNavigate();
  const tracker = useImportTracker();
  const imports = useTrackedImports();
  const create = useMutation({ mutationFn: createImport });
  const upload = useMutation({ mutationFn: uploadPdf });
  const [url, setUrl] = useState("");
  const [file, setFile] = useState<File>();
  const [dragging, setDragging] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [followingId, setFollowingId] = useState<string>();
  const following = imports.find((i) => i.id === followingId);

  useEffect(() => {
    if (following?.status === "done" && following.documentId) {
      dialog.current?.close();
      void navigate({ to: "/doc/$id", params: { id: following.documentId } });
    }
  }, [following, dialog, navigate]);

  // Back to an empty form. An ending the dialog has shown is not kept for
  // the library to show again.
  const reset = () => {
    if (following && endingMessage(following)) {
      tracker.dismiss(following.id);
    }
    setFollowingId(undefined);
    setUrl("");
    setFile(undefined);
    setProblem(undefined);
    create.reset();
    upload.reset();
  };

  const started = {
    onSuccess: (created: CreatedImport) => {
      if (created.existing) {
        dialog.current?.close();
        void navigate({
          to: "/doc/$id",
          params: { id: created.document.id },
          state: { notice: "existing" },
        });
        return;
      }
      tracker.track(created.import);
      setFollowingId(created.import.id);
    },
    onError: (error: unknown) => setProblem(requestErrorMessage(error)),
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const code = urlProblem(url);
    if (code) {
      setProblem(requestErrorMessages[code]);
      return;
    }
    setProblem(undefined);
    create.mutate(url.trim(), started);
  };

  const choose = (chosen: File | undefined) => {
    setFile(chosen);
    setProblem(undefined);
  };

  const submitPdf = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!file) {
      setProblem("Choose a PDF to upload.");
      return;
    }
    const code = pdfProblem(file);
    if (code) {
      setProblem(requestErrorMessages[code]);
      return;
    }
    setProblem(undefined);
    upload.mutate(file, started);
  };

  const drop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    choose(event.dataTransfer.files[0]);
  };

  const problemText = problem && (
    <p id={problemId} className="mt-2 text-ui-lg">
      {problem}
    </p>
  );

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      onClose={reset}
      className="m-auto w-[min(30rem,calc(100vw-2rem))] translate-y-1 rounded-md border border-rule bg-paper p-6 text-ink opacity-0 backdrop:bg-ink/30 open:translate-y-0 open:opacity-100 motion-safe:transition-[opacity,translate,display,overlay] motion-safe:transition-discrete starting:open:translate-y-1 starting:open:opacity-0"
    >
      <h2 id={titleId} className="font-semibold text-h3">
        Import page
      </h2>
      {following ? (
        <Progress state={following} onRetry={reset} />
      ) : (
        <Tabs.Root defaultValue="web" onValueChange={() => setProblem(undefined)} className="mt-4">
          <Tabs.List aria-label="Import from" className="flex gap-1 border-rule border-b pb-2">
            <Tabs.Trigger value="web" className={tabClass}>
              Web page
            </Tabs.Trigger>
            <Tabs.Trigger value="pdf" className={tabClass}>
              Upload PDF
            </Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="web">
            <form noValidate onSubmit={submit} className="mt-4">
              <label htmlFor={inputId} className="block text-ui-lg">
                Page URL
              </label>
              <input
                id={inputId}
                // showModal focuses the dialog's autofocus element. React's
                // autoFocus only calls focus() at mount, while it is closed.
                ref={(el) => el?.setAttribute("autofocus", "")}
                type="url"
                name="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                placeholder="https://"
                aria-invalid={problem ? true : undefined}
                aria-describedby={problem ? problemId : undefined}
                className="mt-1 w-full rounded-sm border border-rule bg-paper px-2 py-1.5 text-ui-lg aria-invalid:border-accent"
              />
              {problemText}
              <div className="mt-4 flex gap-2">
                <button type="submit" disabled={create.isPending} className={buttonClass}>
                  Import
                </button>
                <button
                  type="button"
                  onClick={() => dialog.current?.close()}
                  className={buttonClass}
                >
                  Cancel
                </button>
              </div>
            </form>
          </Tabs.Content>
          <Tabs.Content value="pdf">
            <form noValidate onSubmit={submitPdf} className="mt-4">
              {/* biome-ignore lint/a11y/noStaticElementInteractions: a drop target for dragged files; the file picker inside it is the keyboard's way in */}
              <div
                data-testid="pdf-drop"
                data-dragging={dragging || undefined}
                onDragOver={(e) => {
                  e.preventDefault();
                  setDragging(true);
                }}
                onDragLeave={() => setDragging(false)}
                onDrop={drop}
                className="flex flex-col items-center gap-2 rounded-md border border-rule border-dashed px-4 py-6 text-center text-ui-lg data-dragging:border-accent data-dragging:bg-paper-2"
              >
                <p className="text-ink-2">Drop a PDF here, up to 25 MB, or</p>
                <input
                  id={fileId}
                  type="file"
                  accept="application/pdf,.pdf"
                  onChange={(e) => choose(e.target.files?.[0])}
                  aria-describedby={problem ? problemId : undefined}
                  className="peer sr-only"
                />
                <label
                  htmlFor={fileId}
                  className={`${buttonClass} cursor-pointer peer-focus-visible:outline-2 peer-focus-visible:outline-accent`}
                >
                  Choose a PDF
                </label>
                {file && <p className="max-w-full truncate">{file.name}</p>}
              </div>
              {problemText}
              <div className="mt-4 flex gap-2">
                <button type="submit" disabled={upload.isPending} className={buttonClass}>
                  Upload
                </button>
                <button
                  type="button"
                  onClick={() => dialog.current?.close()}
                  className={buttonClass}
                >
                  Cancel
                </button>
              </div>
            </form>
          </Tabs.Content>
        </Tabs.Root>
      )}
      <button
        type="button"
        onClick={() => dialog.current?.close()}
        aria-label="Close"
        className="absolute top-4 right-4 rounded-sm p-1 text-ink-2 hover:bg-paper-2 hover:text-ink"
      >
        <XIcon size={16} aria-hidden />
      </button>
    </dialog>
  );
}

const stepIcons = {
  complete: <CheckCircleIcon size={16} aria-hidden className="text-accent" />,
  current: <CircleHalfIcon size={16} aria-hidden />,
  waiting: <CircleIcon size={16} aria-hidden className="text-ink-2" />,
};

/** An import's steps as they happen, or why it ended early. */
function Progress({ state, onRetry }: { state: TrackedImport; onRetry: () => void }) {
  const ending = endingMessage(state);
  if (ending) {
    return (
      <div className="mt-4">
        <p role="alert" className="text-ui-lg">
          {ending}
        </p>
        <button type="button" onClick={onRetry} className={`mt-4 ${buttonClass}`}>
          {state.kind === "pdf" ? "Try another file" : "Try another link"}
        </button>
      </div>
    );
  }
  return (
    <div className="mt-4">
      <p className="truncate text-ink-2 text-ui">{state.name}</p>
      <ol aria-label="Progress" className="mt-3 space-y-2">
        {stepsOf(state.kind).map((step) => {
          const progress = stepState(state, step);
          return (
            <li
              key={step}
              data-step={step}
              data-state={progress}
              className="flex items-center gap-2 text-ui-lg data-[state=waiting]:text-ink-2"
            >
              {stepIcons[progress]}
              {stepLabels[step]}
            </li>
          );
        })}
      </ol>
      <p role="status" className="sr-only">
        {state.status === "failed" ? "" : stepLabels[state.status]}
      </p>
    </div>
  );
}

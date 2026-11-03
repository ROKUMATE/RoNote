import { CheckCircleIcon } from "@phosphor-icons/react/CheckCircle";
import { CircleIcon } from "@phosphor-icons/react/Circle";
import { CircleHalfIcon } from "@phosphor-icons/react/CircleHalf";
import { FilePlusIcon } from "@phosphor-icons/react/FilePlus";
import { XIcon } from "@phosphor-icons/react/X";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, type RefObject, useEffect, useId, useRef, useState } from "react";
import { ApiError } from "../api/client";
import { requestErrorMessages, unreachableMessage } from "../api/messages";
import { createImport } from "../api/queries";
import type { ErrorCode } from "../api/schema";
import { buttonClass } from "../ui/button";
import { endingMessage, stepLabels } from "./describe";
import { stepState, steps, type TrackedImport } from "./state";
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

function requestErrorMessage(error: unknown): string {
  return error instanceof ApiError && error.body
    ? requestErrorMessages[error.body.code]
    : unreachableMessage;
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

/**
 * Asks for a URL and follows the import. Closing it never cancels: the
 * tracker keeps following, and the library shows the import until it ends.
 */
function ImportDialog({ dialog }: { dialog: RefObject<HTMLDialogElement | null> }) {
  const titleId = useId();
  const inputId = useId();
  const problemId = useId();
  const navigate = useNavigate();
  const tracker = useImportTracker();
  const imports = useTrackedImports();
  const create = useMutation({ mutationFn: createImport });
  const [url, setUrl] = useState("");
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
    setProblem(undefined);
    create.reset();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const code = urlProblem(url);
    if (code) {
      setProblem(requestErrorMessages[code]);
      return;
    }
    setProblem(undefined);
    create.mutate(url.trim(), {
      onSuccess: (created) => {
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
      onError: (error) => setProblem(requestErrorMessage(error)),
    });
  };

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
        <form noValidate onSubmit={submit} className="mt-4">
          <label htmlFor={inputId} className="block text-ui-lg">
            Page URL
          </label>
          <input
            id={inputId}
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
          {problem && (
            <p id={problemId} className="mt-2 text-ui-lg">
              {problem}
            </p>
          )}
          <div className="mt-4 flex gap-2">
            <button type="submit" disabled={create.isPending} className={buttonClass}>
              Import
            </button>
            <button type="button" onClick={() => dialog.current?.close()} className={buttonClass}>
              Cancel
            </button>
          </div>
        </form>
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
          Try another link
        </button>
      </div>
    );
  }
  return (
    <div className="mt-4">
      <p className="truncate text-ink-2 text-ui">{state.url}</p>
      <ol aria-label="Progress" className="mt-3 space-y-2">
        {steps.map((step) => {
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

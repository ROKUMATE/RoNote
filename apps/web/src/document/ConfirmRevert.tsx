import { useEffect, useId, useRef } from "react";
import { buttonClass } from "../ui/button";

/** Asks before an edited block goes back to the page's text. */
export function ConfirmRevert({
  onConfirm,
  onCancel,
}: {
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    dialog.current?.showModal();
  }, []);

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      onClose={onCancel}
      className="m-auto w-[min(26rem,calc(100vw-2rem))] rounded-md border border-rule bg-paper p-6 text-ink backdrop:bg-ink/30"
    >
      <h2 id={titleId} className="font-semibold text-h3">
        Revert to source?
      </h2>
      <p className="mt-2 text-ui-lg">
        The block goes back to the page's text, and your edits to it are lost.
      </p>
      <div className="mt-4 flex gap-2">
        <button type="button" onClick={onConfirm} className={buttonClass}>
          Revert
        </button>
        <button type="button" onClick={() => dialog.current?.close()} className={buttonClass}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

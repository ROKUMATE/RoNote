import { useEffect, useId, useRef } from "react";
import { buttonClass } from "./button";

/** A modal question before something that can't be undone. */
export function Confirm({
  title,
  children,
  action,
  onConfirm,
  onCancel,
}: {
  title: string;
  children: React.ReactNode;
  /** The confirming button's label, a plain verb. */
  action: string;
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
        {title}
      </h2>
      <div className="mt-2 text-ui-lg">{children}</div>
      <div className="mt-4 flex gap-2">
        <button type="button" onClick={onConfirm} className={buttonClass}>
          {action}
        </button>
        <button type="button" onClick={() => dialog.current?.close()} className={buttonClass}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}

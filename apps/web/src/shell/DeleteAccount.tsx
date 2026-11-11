import { useEffect, useId, useRef, useState } from "react";
import { requestErrorMessage } from "../api/messages";
import { deleteAccount } from "../api/queries";
import { leave } from "../api/session";
import { buttonClass } from "../ui/button";

/**
 * Asks before the account goes, and what goes with it. Deleting needs the
 * GitHub login typed out, so it can't happen by a stray click.
 */
export function DeleteAccount({ login, onCancel }: { login: string; onCancel: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const inputId = useId();
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<string>();
  const [deleting, setDeleting] = useState(false);
  const confirmed = typed.trim().toLowerCase() === login.toLowerCase();

  useEffect(() => {
    dialog.current?.showModal();
  }, []);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!confirmed) return;
    setDeleting(true);
    setError(undefined);
    try {
      await deleteAccount(typed.trim());
      leave("/login");
    } catch (e) {
      setError(requestErrorMessage(e));
      setDeleting(false);
    }
  };

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      onClose={onCancel}
      className="m-auto w-[min(28rem,calc(100vw-2rem))] rounded-md border border-rule bg-paper p-6 text-ink backdrop:bg-ink/30"
    >
      <form onSubmit={submit}>
        <h2 id={titleId} className="font-semibold text-h3">
          Delete your account?
        </h2>
        <p className="mt-2 text-ui-lg">
          This removes your documents, your imports, every highlight and note, and signs you out
          everywhere. It can't be undone. Pages that other people imported stay theirs.
        </p>
        <label htmlFor={inputId} className="mt-4 block text-ui-lg">
          Type your GitHub login, <strong>{login}</strong>, to confirm
        </label>
        <input
          id={inputId}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          autoComplete="off"
          spellCheck={false}
          className="mt-1 w-full rounded-sm border border-rule bg-paper px-2 py-1.5 text-ui-lg"
        />
        {error && (
          <p role="alert" className="mt-2 text-ui-lg">
            {error}
          </p>
        )}
        <div className="mt-4 flex gap-2">
          <button type="submit" disabled={!confirmed || deleting} className={buttonClass}>
            Delete account
          </button>
          <button type="button" onClick={() => dialog.current?.close()} className={buttonClass}>
            Cancel
          </button>
        </div>
      </form>
    </dialog>
  );
}

import { UserCircleIcon } from "@phosphor-icons/react/UserCircle";
import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { requestErrorMessage } from "../api/messages";
import { meQuery, signOut, signOutEverywhere } from "../api/queries";
import { leave } from "../api/session";
import { DeleteAccount } from "./DeleteAccount";

const item =
  "block w-full rounded-sm px-2 py-1 text-left text-ui-lg hover:bg-paper-2 disabled:opacity-60";

/**
 * The signed-in user's name, and a menu to sign out or delete the account.
 * The native popover API provides light dismiss, Escape and focus order, as
 * in Settings. No avatar: nothing loads from GitHub.
 */
export function Account() {
  const popoverId = useId();
  const me = useQuery(meQuery);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (!me.data) return null;

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    setError(undefined);
    try {
      await action();
      leave("/login");
    } catch (e) {
      setError(requestErrorMessage(e));
      setBusy(false);
    }
  };

  return (
    <>
      <button
        type="button"
        popoverTarget={popoverId}
        className="flex min-w-0 items-center gap-2 rounded-sm px-2 py-1 text-ink-2 text-ui-lg hover:bg-paper-2 hover:text-ink"
      >
        <UserCircleIcon size={16} aria-hidden className="shrink-0" />
        <span className="truncate">{me.data.display_name}</span>
      </button>
      <div
        id={popoverId}
        popover="auto"
        role="dialog"
        aria-label="Account"
        className="fixed inset-auto top-12 right-2 m-0 w-60 space-y-1 rounded-md border border-rule bg-paper p-2 text-ink opacity-0 open:opacity-100 sidebar:top-auto sidebar:right-auto sidebar:bottom-12 sidebar:left-4 motion-safe:transition-[opacity,display,overlay] motion-safe:transition-discrete starting:open:opacity-0"
      >
        <p className="truncate px-2 py-1 text-ink-2 text-ui">Signed in as {me.data.login}</p>
        <button type="button" disabled={busy} onClick={() => run(signOut)} className={item}>
          Sign out
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => run(signOutEverywhere)}
          className={item}
        >
          Sign out everywhere
        </button>
        <button type="button" disabled={busy} onClick={() => setDeleting(true)} className={item}>
          Delete account…
        </button>
        {error && (
          <p role="alert" className="px-2 py-1 text-ui">
            {error}
          </p>
        )}
      </div>
      {deleting && <DeleteAccount login={me.data.login} onCancel={() => setDeleting(false)} />}
    </>
  );
}

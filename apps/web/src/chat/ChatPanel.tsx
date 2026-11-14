import { ListIcon } from "@phosphor-icons/react/List";
import { PlusIcon } from "@phosphor-icons/react/Plus";
import { TrashIcon } from "@phosphor-icons/react/Trash";
import { XIcon } from "@phosphor-icons/react/X";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { requestErrorMessage } from "../api/messages";
import { chatKey, conversationsQuery, deleteConversation } from "../api/queries";
import { formatDate } from "../format";
import { Conversation, useLanding } from "./Conversation";

/** At the notes breakpoint and wider the panel sits beside the page; below, it covers it. */
const besidePage = "(min-width: 68.75rem)";

const iconButton =
  "inline-flex items-center gap-1.5 rounded-sm px-2 py-1 text-ink-2 text-ui hover:bg-paper-2 hover:text-ink";

/**
 * Chat over the library (Mod+J, ADR 0012): a panel on the right, over the
 * notes margin, or a full-height sheet below the notes breakpoint. It shows
 * one conversation at a time, or the list of them. Opened from a document,
 * a new conversation can be about that document alone.
 */
export function ChatPanel({ documentId, onClose }: { documentId?: string; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const [view, setView] = useState<"conversation" | "list">("conversation");
  const [conversationId, setConversationId] = useState<string>();
  // Each conversation opened, or started, is a fresh view; one started here
  // keeps its view when it gets its id, mid-answer.
  const [opened, setOpened] = useState(0);
  // A new conversation starts in the scope of where the panel opened.
  const [startedIn, setStartedIn] = useState(documentId);
  const land = useLanding(() => {
    if (!window.matchMedia?.(besidePage).matches) dialog.current?.close();
  });

  useEffect(() => {
    const d = dialog.current;
    if (!d) return;
    // Beside the page the panel leaves it usable; over it, it is modal.
    if (window.matchMedia?.(besidePage).matches ?? true) d.show();
    else d.showModal();
    d.querySelector("textarea")?.focus();
    // Escape closes the panel from inside it, and when focus has fallen to
    // the page, as when what held it was redrawn; not from the reader.
    const onKey = (event: KeyboardEvent) => {
      const at = document.activeElement;
      if (event.key === "Escape" && (at === document.body || d.contains(at))) {
        event.preventDefault();
        d.close();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const startNew = () => {
    setConversationId(undefined);
    setStartedIn(documentId);
    setOpened((n) => n + 1);
    setView("conversation");
  };

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      onClose={onClose}
      className="fixed inset-0 z-20 m-0 ml-auto flex h-dvh max-h-none w-full max-w-none flex-col border-rule bg-paper p-0 text-ink backdrop:bg-ink/30 notes:w-96 notes:border-l [&:not([open])]:hidden"
    >
      <div className="flex items-center gap-1 border-rule border-b px-4 py-2">
        <h2 id={titleId} className="mr-auto font-semibold text-ui-lg">
          Chat
        </h2>
        <button
          type="button"
          onClick={() => setView(view === "list" ? "conversation" : "list")}
          aria-pressed={view === "list"}
          className={iconButton}
        >
          <ListIcon size={16} aria-hidden />
          Conversations
        </button>
        <button type="button" onClick={startNew} className={iconButton}>
          <PlusIcon size={16} aria-hidden />
          New
        </button>
        <button
          type="button"
          onClick={() => dialog.current?.close()}
          aria-label="Close chat"
          className={iconButton}
        >
          <XIcon size={16} aria-hidden />
        </button>
      </div>
      {view === "list" ? (
        <Conversations
          current={conversationId}
          onOpen={(id) => {
            setConversationId(id);
            setOpened((n) => n + 1);
            setView("conversation");
          }}
          onDeleted={(id) => {
            if (id === conversationId) setConversationId(undefined);
          }}
        />
      ) : (
        <Available>
          <Conversation
            key={opened}
            {...(conversationId ? { id: conversationId } : {})}
            {...(startedIn ? { documentId: startedIn } : {})}
            onStarted={setConversationId}
            onLand={land}
          />
        </Available>
      )}
    </dialog>
  );
}

/** Shows children unless chat is off or can't be reached. */
function Available({ children }: { children: React.ReactNode }) {
  const list = useQuery(conversationsQuery);
  if (list.isError) {
    return (
      <p role="alert" className="px-4 py-3 text-ui-lg">
        {requestErrorMessage(list.error)}
      </p>
    );
  }
  return children;
}

/** The user's latest conversations, newest first. */
function Conversations({
  current,
  onOpen,
  onDeleted,
}: {
  current: string | undefined;
  onOpen: (id: string) => void;
  onDeleted: (id: string) => void;
}) {
  const queryClient = useQueryClient();
  const list = useQuery(conversationsQuery);
  const [failed, setFailed] = useState<string>();
  const remove = async (id: string) => {
    try {
      await deleteConversation(id);
      onDeleted(id);
      await queryClient.invalidateQueries({ queryKey: chatKey });
    } catch (error) {
      setFailed(requestErrorMessage(error));
    }
  };
  if (list.isError) {
    return (
      <p role="alert" className="px-4 py-3 text-ui-lg">
        {requestErrorMessage(list.error)}
      </p>
    );
  }
  const conversations = list.data?.conversations ?? [];
  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
      {failed && (
        <p role="alert" className="px-2 py-1 text-ui-lg">
          {failed}
        </p>
      )}
      {list.isSuccess && conversations.length === 0 && (
        <p className="px-2 py-1 text-ink-2 text-ui-lg">No conversations yet.</p>
      )}
      <ul aria-label="Conversations" className="space-y-0.5">
        {conversations.map((c) => {
          const title = c.title || "New conversation";
          return (
            <li key={c.id} className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => onOpen(c.id)}
                aria-current={c.id === current ? "true" : undefined}
                className="min-w-0 flex-1 rounded-sm px-2 py-1.5 text-left hover:bg-paper-2 aria-[current=true]:bg-paper-2"
              >
                <span className="block truncate text-ui-lg">{title}</span>
                <span className="block truncate text-ink-2 text-ui">
                  {c.document_title ?? "Library"} · {formatDate(c.updated_at)}
                </span>
              </button>
              <button
                type="button"
                onClick={() => void remove(c.id)}
                aria-label={`Delete conversation: ${title}`}
                className={iconButton}
              >
                <TrashIcon size={16} aria-hidden />
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

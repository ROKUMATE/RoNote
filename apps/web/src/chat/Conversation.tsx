import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { type ChatEvent, sendMessage } from "../api/chat";
import { chatErrorMessages, requestErrorMessage } from "../api/messages";
import {
  chatKey,
  conversationQuery,
  conversationsQuery,
  createConversation,
  stopAnswer,
} from "../api/queries";
import type { ChatErrorCode, Citation, Message, MessageSegment } from "../api/schema";
import { buttonClass } from "../ui/button";
import { ChoiceGroup } from "../ui/ChoiceGroup";
import { Markdown } from "./markdown";

/** The most a question may say, as the API allows. */
const maxQuestion = 2000;

/** What an answer says when the model said the library doesn't answer (ADR 0012). */
export const notFoundLabel = "Not found in your library";

/** What an answer says when it has text and cites nothing: it was not checked. */
export const unsourcedLabel = "No sources";
export const unsourcedNote = "This answer couldn't be checked against your library.";

/** An answer as it streams in, before it is saved. */
type Live = {
  question: string;
  segments: MessageSegment[];
  citations: Citation[];
  searching?: string | undefined;
  error?: ChatErrorCode;
};

/** Where a new conversation looks: the open document, or the library. */
type Scope = "document" | "library";

/**
 * One conversation: its messages, the answer being written, and the box to
 * ask in. Without an id it is a new conversation, made with the first
 * question, in the scope chosen.
 */
export function Conversation({
  id,
  documentId,
  onStarted,
  onLand,
}: {
  id?: string;
  documentId?: string;
  onStarted: (id: string) => void;
  onLand: (citation: Citation) => void;
}) {
  const queryClient = useQueryClient();
  const [live, setLive] = useState<Live>();
  const query = conversationQuery(id ?? "");
  const conversation = useQuery({
    ...query,
    enabled: id !== undefined,
    // The answer streaming here needs no polling.
    ...(live ? { refetchInterval: false as const } : {}),
  });
  const list = useQuery(conversationsQuery);
  const [scope, setScope] = useState<Scope>(documentId ? "document" : "library");
  const [typed, setTyped] = useState("");
  const [refused, setRefused] = useState<string>();
  // While an answer streams here, the messages before its question.
  const [before, setBefore] = useState(0);
  const reading = useRef<AbortController>(undefined);
  const inputId = useId();
  const saved = conversation.data?.messages ?? [];
  // The question and answer streaming here show live, not as saved so far.
  const messages = live ? saved.slice(0, before) : saved;
  const writing = live !== undefined || saved.some((m) => m.status === "streaming");
  const left = list.data?.messages_left_today;

  // Leaving stops reading the stream; the server finishes and saves it.
  useEffect(() => () => reading.current?.abort(), []);

  const ask = async (question: string) => {
    setRefused(undefined);
    setBefore(saved.length);
    setLive({ question, segments: [], citations: [] });
    let conversationId = id;
    try {
      if (!conversationId) {
        conversationId = (await createConversation(scope === "document" ? documentId : undefined))
          .id;
        onStarted(conversationId);
      }
      reading.current = new AbortController();
      await sendMessage(conversationId, question, hear, reading.current.signal);
      await queryClient.invalidateQueries({ queryKey: chatKey });
      setLive(undefined);
    } catch (error) {
      if (reading.current?.signal.aborted) return;
      setLive(undefined);
      setTyped(question);
      setRefused(requestErrorMessage(error));
    }
  };

  const hear = (event: ChatEvent) => {
    switch (event.type) {
      case "delta":
        setLive((l) => l && { ...l, searching: undefined, segments: grow(l.segments, event.data) });
        break;
      case "citation": {
        const { segment, citation } = event.data;
        setLive(
          (l) =>
            l && {
              ...l,
              citations: l.citations.some((c) => c.n === citation.n)
                ? l.citations
                : [...l.citations, citation],
              segments: cite(l.segments, segment, citation.n),
            },
        );
        break;
      }
      case "tool":
        setLive((l) => l && { ...l, searching: event.data.query });
        break;
      case "error":
        setLive((l) => l && { ...l, error: event.data.code });
        break;
      case "done":
        queryClient.setQueryData(conversationsQuery.queryKey, (old) =>
          old ? { ...old, messages_left_today: event.data.messages_left_today } : old,
        );
        break;
    }
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const question = typed.trim();
    if (question === "" || writing) return;
    setTyped("");
    void ask(question);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      submit(event);
    }
  };

  const scopeLabel = conversation.data?.conversation.document_title
    ? `This document: ${conversation.data.conversation.document_title}`
    : "Library";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="border-rule border-b px-4 py-2">
        {id === undefined && documentId ? (
          <ChoiceGroup
            legend="Ask about"
            name="chat-scope"
            options={[
              ["document", "This document"],
              ["library", "Library"],
            ]}
            value={scope}
            onChange={setScope}
          />
        ) : (
          <p className="text-ink-2 text-ui">
            {id === undefined ? "Ask about your library" : `Asking about: ${scopeLabel}`}
          </p>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-4 py-3">
        {messages.length === 0 && !live && (
          <p className="text-ink-2 text-ui-lg">
            Ask a question. Answers come from your imported pages and your notes, with the blocks
            they cite.
          </p>
        )}
        <ol aria-label="Messages" aria-busy={writing} className="space-y-4">
          {messages.map((m) => (
            <li key={m.id}>
              {m.role === "user" ? (
                <Question text={m.segments.map((s) => s.text).join("")} />
              ) : (
                <Answer message={m} onLand={onLand} />
              )}
            </li>
          ))}
          {live && (
            <>
              <li>
                <Question text={live.question} />
              </li>
              <li>
                <LiveAnswer live={live} onLand={onLand} />
              </li>
            </>
          )}
        </ol>
        <p role="status" className="sr-only">
          {writing ? "Writing the answer." : messages.length > 0 ? "Answer written." : ""}
        </p>
      </div>
      <form onSubmit={submit} className="border-rule border-t px-4 py-3">
        {refused && (
          <p role="alert" className="mb-2 text-ui-lg">
            {refused}
          </p>
        )}
        <label htmlFor={inputId} className="sr-only">
          Ask your library
        </label>
        <textarea
          id={inputId}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          onKeyDown={onKeyDown}
          maxLength={maxQuestion}
          rows={2}
          disabled={left === 0}
          placeholder="Ask your library"
          className="w-full resize-none rounded-sm border border-rule bg-paper px-2 py-1.5 text-ui-lg placeholder:text-ink-2 disabled:opacity-60"
        />
        <div className="mt-2 flex items-center gap-3">
          {writing && id ? (
            <button type="button" onClick={() => void stopAnswer(id)} className={buttonClass}>
              Stop
            </button>
          ) : (
            <button
              type="submit"
              disabled={typed.trim() === "" || writing || left === 0}
              className={buttonClass}
            >
              Ask
            </button>
          )}
          {left !== undefined && (
            <p className="text-ink-2 text-ui">
              {left === 0
                ? "No questions left today. More free up through the day."
                : `${left} ${left === 1 ? "question" : "questions"} left today`}
            </p>
          )}
        </div>
      </form>
    </div>
  );
}

/** Adds a delta's text to its segment. */
function grow(segments: MessageSegment[], { segment, text }: { segment: number; text: string }) {
  const out = [...segments];
  while (out.length <= segment) out.push({ text: "", citations: [] });
  const s = out[segment] ?? { text: "", citations: [] };
  out[segment] = { ...s, text: s.text + text };
  return out;
}

/** Adds citation n to its segment. */
function cite(segments: MessageSegment[], segment: number, n: number) {
  const out = grow(segments, { segment, text: "" });
  const s = out[segment] ?? { text: "", citations: [] };
  if (!s.citations.includes(n)) out[segment] = { ...s, citations: [...s.citations, n] };
  return out;
}

function Question({ text }: { text: string }) {
  return (
    <p className="ml-6 whitespace-pre-wrap rounded-sm bg-paper-2 px-3 py-2 text-ui-lg">{text}</p>
  );
}

/** Machine-made text: a thin left rule and a "generated" label (DESIGN.md). */
function Generated({ muted, children }: { muted?: boolean; children: React.ReactNode }) {
  return (
    <div className={`border-gen border-l-2 pl-3 text-ui-lg ${muted ? "text-ink-2" : ""}`}>
      <span className="block text-gen text-ui">generated</span>
      {children}
    </div>
  );
}

/** A saved answer, as it ended. */
function Answer({ message, onLand }: { message: Message; onLand: (c: Citation) => void }) {
  const said = message.segments.some((s) => s.text.trim() !== "");
  if (message.status === "streaming") {
    return (
      <Generated muted>
        <p className="my-2">Still writing this answer…</p>
      </Generated>
    );
  }
  const notFound = message.status === "not_found";
  const unsourced = message.status === "unsourced";
  return (
    <Generated muted={notFound || unsourced}>
      {notFound && <p className="mt-1 font-semibold">{notFoundLabel}</p>}
      {unsourced && <p className="mt-1 font-semibold">{unsourcedLabel}</p>}
      {notFound && message.not_found && (
        <>
          <p className="my-2">{message.not_found.missing}</p>
          <p className="my-2">To find it, import {message.not_found.suggest}.</p>
        </>
      )}
      {said && !notFound && (
        <Markdown segments={message.segments} citations={message.citations} onLand={onLand} />
      )}
      {unsourced && <p className="my-2 text-ink-2 text-ui">{unsourcedNote}</p>}
      {message.status === "stopped" && <p className="my-2 text-ink-2 text-ui">Stopped.</p>}
      {message.status === "failed" && (
        <p className="my-2 text-ui-lg">{chatErrorMessages[message.error ?? "internal_error"]}</p>
      )}
    </Generated>
  );
}

function LiveAnswer({ live, onLand }: { live: Live; onLand: (c: Citation) => void }) {
  return (
    <Generated>
      {live.segments.length === 0 && !live.searching && (
        <p className="my-2 text-ink-2">Reading your library…</p>
      )}
      <Markdown segments={live.segments} citations={live.citations} onLand={onLand} />
      {live.searching && (
        <p className="my-2 text-ink-2 text-ui">Searching your library for “{live.searching}”…</p>
      )}
      {live.error && <p className="my-2 text-ui-lg">{chatErrorMessages[live.error]}</p>}
    </Generated>
  );
}

/** Lands on a citation's first block, as a search result does (ADR 0011). */
export function useLanding(onLeave: () => void) {
  const navigate = useNavigate();
  return (citation: Citation) => {
    onLeave();
    void navigate({
      to: "/doc/$id",
      params: { id: citation.document_id },
      search: citation.block_ids[0] ? { block: citation.block_ids[0] } : {},
    });
  };
}

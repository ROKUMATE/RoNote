import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";
import type { ChatDone, Citation, Conversation, Message } from "../api/schema";
import { apiError, json, stubApi } from "../test/api";
import { renderApp } from "../test/render";
import { notFoundLabel } from "./Conversation";

const created = "2026-11-14T09:00:00Z";
const conversation: Conversation = {
  id: "c1",
  title: "How do goroutines talk?",
  created_at: created,
  updated_at: created,
};
const channels: Citation = {
  n: 1,
  document_id: "doc-go",
  block_ids: ["b-channels"],
  cited_text: "Channels let goroutines communicate.",
  title: "Effective Go > Channels",
};
const question: Message = {
  id: "m1",
  role: "user",
  status: "done",
  segments: [{ text: "How do goroutines talk?", citations: [] }],
  citations: [],
  created_at: created,
};

function answer(over: Partial<Message> = {}): Message {
  return {
    id: "m2",
    role: "assistant",
    status: "done",
    segments: [{ text: "Goroutines use channels.", citations: [1] }],
    citations: [channels],
    created_at: created,
    ...over,
  };
}

/** A stream the test writes events into, then closes. */
function eventStream() {
  const encoder = new TextEncoder();
  let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  return {
    response: () => new Response(body, { headers: { "Content-Type": "text/event-stream" } }),
    send(event: string, data: unknown) {
      controller?.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`));
    },
    close() {
      controller?.close();
    },
  };
}

/** The events of a whole answer, as one response. */
function answered(message: Message, left = 49) {
  const s = eventStream();
  message.segments.forEach((segment, i) => {
    s.send("delta", { segment: i, text: segment.text });
    for (const n of segment.citations) {
      const citation = message.citations.find((c) => c.n === n);
      if (citation) s.send("citation", { segment: i, citation });
    }
  });
  if (message.error) s.send("error", { code: message.error });
  s.send("done", { message, messages_left_today: left } satisfies ChatDone);
  s.close();
  return s.response();
}

async function openChat() {
  await screen.findByRole("button", { name: /^Chat/ });
  fireEvent.keyDown(window, { key: "j", metaKey: true });
  return screen.findByRole("dialog", { name: "Chat" });
}

async function ask(panel: HTMLElement, text: string) {
  const box = await within(panel).findByRole("textbox", { name: "Ask your library" });
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

test("Mod+J opens the chat; an answer streams with a citation that lands on its block", async () => {
  let saved: Message[] = [];
  const requests = stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () =>
      json({ conversations: [], messages_left_today: saved.length > 0 ? 49 : 50 }),
    "POST /conversations": () => json(conversation, 201),
    "POST /conversations/c1/messages": () => {
      saved = [question, answer()];
      return answered(answer());
    },
    "GET /conversations/c1": () => json({ conversation, messages: saved }),
    "GET /documents/doc-go": () => apiError(404, "not_found"),
  });
  const app = renderApp("/");
  const panel = await openChat();
  await ask(panel, "  How do goroutines talk? ");

  const chip = await within(panel).findByRole("button", {
    name: "Source 1: Effective Go > Channels",
  });
  expect(within(panel).getByText("generated")).toBeTruthy();
  expect(within(panel).getByText("Goroutines use channels.")).toBeTruthy();
  await within(panel).findByText("49 questions left today");
  const sent = requests.find((r) => r.url.endsWith("/conversations/c1/messages"));
  expect(await sent?.clone().json()).toEqual({ text: "How do goroutines talk?" });
  expect(
    await requests
      .find((r) => r.method === "POST" && r.url.endsWith("/conversations"))
      ?.clone()
      .json(),
  ).toEqual({});

  fireEvent.click(chip);
  await waitFor(() => expect(app.router.state.location.href).toBe("/doc/doc-go?block=b-channels"));
});

test("a not_found answer says what the library lacks and what to import", async () => {
  const notFound = answer({
    status: "not_found",
    segments: [],
    citations: [],
    not_found: { missing: "The library has nothing on ServeMux.", suggest: "Go's net/http page" },
  });
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations: [], messages_left_today: 50 }),
    "POST /conversations": () => json(conversation, 201),
    "POST /conversations/c1/messages": () => answered(notFound),
    "GET /conversations/c1": () => json({ conversation, messages: [question, notFound] }),
  });
  renderApp("/");
  const panel = await openChat();
  await ask(panel, "How do I configure ServeMux routes?");
  expect(await within(panel).findByText(notFoundLabel)).toBeTruthy();
  expect(within(panel).getByText("The library has nothing on ServeMux.")).toBeTruthy();
  expect(within(panel).getByText("To find it, import Go's net/http page.")).toBeTruthy();
  expect(within(panel).queryByRole("button", { name: /^Source/ })).toBeNull();
});

test("Stop asks the server to stop, and the answer shows as stopped", async () => {
  const stream = eventStream();
  let saved: Message[] = [];
  const requests = stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations: [], messages_left_today: 50 }),
    "POST /conversations": () => json(conversation, 201),
    "POST /conversations/c1/messages": () => {
      stream.send("delta", { segment: 0, text: "Partly" });
      return stream.response();
    },
    "POST /conversations/c1/stop": () => {
      const stopped = answer({
        status: "stopped",
        segments: [{ text: "Partly", citations: [] }],
        citations: [],
      });
      saved = [question, stopped];
      stream.send("done", { message: stopped, messages_left_today: 49 });
      stream.close();
      return new Response(null, { status: 204 });
    },
    "GET /conversations/c1": () => json({ conversation, messages: saved }),
  });
  renderApp("/");
  const panel = await openChat();
  await ask(panel, "Tell me slowly");
  await within(panel).findByText("Partly");
  fireEvent.click(await within(panel).findByRole("button", { name: "Stop" }));
  expect(await within(panel).findByText("Stopped.")).toBeTruthy();
  expect(requests.some((r) => r.url.endsWith("/conversations/c1/stop"))).toBe(true);
});

test("with no questions left today, asking is off", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations: [], messages_left_today: 0 }),
  });
  renderApp("/");
  const panel = await openChat();
  expect(await within(panel).findByText(/No questions left today/)).toBeTruthy();
  expect(
    (within(panel).getByRole("textbox", { name: "Ask your library" }) as HTMLTextAreaElement)
      .disabled,
  ).toBe(true);
});

test("a question over the quota says so, and keeps the question", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations: [conversation], messages_left_today: 3 }),
    "POST /conversations": () => json(conversation, 201),
    "POST /conversations/c1/messages": () => apiError(429, "quota_exceeded"),
  });
  renderApp("/");
  const panel = await openChat();
  await ask(panel, "One more?");
  expect((await within(panel).findByRole("alert")).textContent).toMatch(/daily limit/);
  expect(
    (within(panel).getByRole("textbox", { name: "Ask your library" }) as HTMLTextAreaElement).value,
  ).toBe("One more?");
});

test("with chat turned off, the panel says so", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => apiError(503, "chat_disabled"),
  });
  renderApp("/");
  const panel = await openChat();
  expect((await within(panel).findByRole("alert")).textContent).toBe(
    "Chat is turned off right now.",
  );
  expect(within(panel).queryByRole("textbox")).toBeNull();
});

test("conversations are listed, opened and deleted", async () => {
  let conversations = [conversation];
  const requests = stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations, messages_left_today: 50 }),
    "GET /conversations/c1": () => json({ conversation, messages: [question, answer()] }),
    "DELETE /conversations/c1": () => {
      conversations = [];
      return new Response(null, { status: 204 });
    },
  });
  renderApp("/");
  const panel = await openChat();
  fireEvent.click(within(panel).getByRole("button", { name: "Conversations" }));
  const list = await within(panel).findByRole("list", { name: "Conversations" });
  fireEvent.click(within(list).getByRole("button", { name: /^How do goroutines talk\?/ }));
  expect(await within(panel).findByText("Goroutines use channels.")).toBeTruthy();
  expect(within(panel).getByText("Asking about: Library")).toBeTruthy();

  fireEvent.click(within(panel).getByRole("button", { name: "Conversations" }));
  fireEvent.click(
    await within(panel).findByRole("button", {
      name: "Delete conversation: How do goroutines talk?",
    }),
  );
  expect(await within(panel).findByText("No conversations yet.")).toBeTruthy();
  expect(requests.some((r) => r.method === "DELETE")).toBe(true);
});

test("opened on a document, a conversation can be about it or the whole library", async () => {
  const bodies: unknown[] = [];
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () =>
      json({
        id: "doc-1",
        title: "Guide",
        source_url: "https://docs.example.com/guide",
        version: 1,
        created_at: created,
        updated_at: created,
        content: {
          doc_version: 1,
          blocks: [
            {
              id: "p",
              kind: "paragraph",
              content: [{ text: "Hello" }],
              origin: "source",
              edited: false,
              source_block_id: "p",
            },
          ],
        },
        warnings: [],
      }),
    "GET /documents/doc-1/annotations": () => json({ annotations: [] }),
    "POST /documents/resolve": () => json({ links: [] }),
    "GET /conversations": () => json({ conversations: [], messages_left_today: 50 }),
    "POST /conversations": async (r) => {
      bodies.push(await r.json());
      return json({ ...conversation, document_id: "doc-1", document_title: "Guide" }, 201);
    },
    "POST /conversations/c1/messages": () => answered(answer()),
    "GET /conversations/c1": () =>
      json({
        conversation: { ...conversation, document_id: "doc-1", document_title: "Guide" },
        messages: [question, answer()],
      }),
  });
  renderApp("/doc/doc-1");
  const panel = await openChat();
  const scope = within(panel).getByRole("group", { name: "Ask about" });
  expect(
    (within(scope).getByRole("radio", { name: "This document" }) as HTMLInputElement).checked,
  ).toBe(true);
  await ask(panel, "What is this page about?");
  await within(panel).findByText("Asking about: This document: Guide");
  expect(bodies).toEqual([{ document_id: "doc-1" }]);
});

test("Escape closes the chat when focus has left it for the page", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /conversations": () => json({ conversations: [], messages_left_today: 50 }),
  });
  renderApp("/");
  const panel = await openChat();
  // As when the answer a focused chip belonged to is replaced by its saved copy.
  (document.activeElement as HTMLElement | null)?.blur();
  expect(document.activeElement).toBe(document.body);
  fireEvent.keyDown(document.body, { key: "Escape" });
  await waitFor(() => expect(panel.isConnected).toBe(false));
});

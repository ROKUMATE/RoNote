import { expect, test } from "vitest";
import { apiError, stubApi } from "../test/api";
import { type ChatEvent, readEvents, sendMessage } from "./chat";
import { ApiError } from "./client";

/** A body that arrives in these chunks. */
function body(...chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
}

test("events are read whole, however the chunks split them", async () => {
  const events: ChatEvent[] = [];
  await readEvents(
    body(
      'event: delta\ndata: {"segment":0,"te',
      'xt":"Goroutines"}\n\n: heartbeat\n\nevent: tool\ndata: {"query":"select"}\n',
      "\nevent: unknown\ndata: {}\n\n",
      'event: delta\ndata: {"segment":0,"text":" — ünïcode"}\n\nevent: done\ndata: {"message":null,"messages_left_today":3}',
    ),
    (event) => events.push(event),
  );
  expect(events).toEqual([
    { type: "delta", data: { segment: 0, text: "Goroutines" } },
    { type: "tool", data: { query: "select" } },
    { type: "delta", data: { segment: 0, text: " — ünïcode" } },
    { type: "done", data: { message: null, messages_left_today: 3 } },
  ]);
});

test("a multi-byte character split across chunks stays whole", async () => {
  const bytes = new TextEncoder().encode('event: delta\ndata: {"segment":0,"text":"é"}\n\n');
  const at = bytes.indexOf(0xc3) + 1;
  const events: ChatEvent[] = [];
  await readEvents(
    new ReadableStream({
      start(controller) {
        controller.enqueue(bytes.slice(0, at));
        controller.enqueue(bytes.slice(at));
        controller.close();
      },
    }),
    (event) => events.push(event),
  );
  expect(events).toEqual([{ type: "delta", data: { segment: 0, text: "é" } }]);
});

test("a refused question rejects with the API's error", async () => {
  stubApi({ "POST /conversations/c1/messages": () => apiError(409, "conversation_busy") });
  const refused = sendMessage("c1", "again?", () => {});
  await expect(refused).rejects.toBeInstanceOf(ApiError);
  await expect(refused).rejects.toMatchObject({ status: 409, body: { code: "conversation_busy" } });
});

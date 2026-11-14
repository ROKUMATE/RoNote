import { api, dataOrThrow } from "./client";
import type { ChatCitation, ChatDelta, ChatDone, ChatError, ChatTool } from "./schema";

/** What an answer's stream says, in order (docs/adr/0012-chat.md). */
export type ChatEvent =
  | { type: "delta"; data: ChatDelta }
  | { type: "citation"; data: ChatCitation }
  | { type: "tool"; data: ChatTool }
  | { type: "done"; data: ChatDone }
  | { type: "error"; data: ChatError };

const eventTypes: ReadonlySet<string> = new Set(["delta", "citation", "tool", "done", "error"]);

/**
 * Asks question in conversation id and calls onEvent with each event of
 * its answer as it arrives; resolves when the stream ends. A refused
 * question rejects with its ApiError: 409 while another answer is written,
 * 429 over the day's quota, 503 with chat off. Aborting signal stops
 * reading, not the answer, which the server finishes and saves; stopping
 * it is stopAnswer.
 */
export async function sendMessage(
  id: string,
  question: string,
  onEvent: (event: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const body = dataOrThrow(
    await api.POST("/conversations/{id}/messages", {
      params: { path: { id } },
      body: { text: question },
      parseAs: "stream",
      ...(signal ? { signal } : {}),
    }),
  );
  if (!body) throw new Error("the answer came without a body");
  await readEvents(body, onEvent);
}

/**
 * Reads server-sent events from body, however its chunks split them, and
 * calls onEvent with each it knows. Comment lines, such as heartbeats, and
 * other events are skipped.
 */
export async function readEvents(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: ChatEvent) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    buffer += done ? `${decoder.decode()}\n\n` : decoder.decode(value, { stream: true });
    let end = buffer.indexOf("\n\n");
    while (end >= 0) {
      const event = parse(buffer.slice(0, end));
      if (event) onEvent(event);
      buffer = buffer.slice(end + 2);
      end = buffer.indexOf("\n\n");
    }
    if (done) return;
  }
}

function parse(block: string): ChatEvent | undefined {
  let type = "";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) type = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!eventTypes.has(type) || data.length === 0) return undefined;
  return { type, data: JSON.parse(data.join("\n")) } as ChatEvent;
}

import { parseEvent, type RunnerEvent, type RunnerRequest } from "../../runner/protocol";

/*
 * The app's side of the runner (docs/adr/0008-in-browser-code-execution.md):
 * a hidden frame on the runner origin, talked to by postMessage only. Events
 * count only from that frame on that origin, and only once they parse.
 */

/** How long the runner has to answer hello before it counts as down. */
const helloTimeout = 10_000;

export type Runner = {
  /** Whether Stop can interrupt code, rather than restart its session. */
  isolated: boolean;
  send(request: RunnerRequest): void;
  /** Calls listener with every event the runner sends; returns a function that stops it. */
  listen(listener: (event: RunnerEvent) => void): () => void;
  /** Removes the frame, and the session in it. */
  close(): void;
};

/** Opens a runner in a hidden frame, resolving once it has said hello. */
export function openRunner(origin = import.meta.env.RONOTE_RUNNER_ORIGIN): Promise<Runner> {
  const frame = document.createElement("iframe");
  frame.src = `${origin}/?${new URLSearchParams({ app: location.origin })}`;
  frame.title = "Code runner";
  frame.hidden = true;
  frame.setAttribute("sandbox", "allow-scripts allow-same-origin");
  frame.allow = "cross-origin-isolated";

  const listeners = new Set<(event: RunnerEvent) => void>();
  const onMessage = (message: MessageEvent) => {
    if (message.origin !== origin || message.source !== frame.contentWindow) return;
    const event = parseEvent(message.data);
    if (!event) return;
    for (const listener of listeners) listener(event);
  };
  const listen = (listener: (event: RunnerEvent) => void) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  };
  const send = (request: RunnerRequest) => frame.contentWindow?.postMessage(request, origin);
  const close = () => {
    window.removeEventListener("message", onMessage);
    listeners.clear();
    frame.remove();
  };

  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      close();
      reject(new Error("The code runner didn't answer."));
    }, helloTimeout);
    const stop = listen((event) => {
      if (event.type !== "hello") return;
      stop();
      clearTimeout(timer);
      resolve({ isolated: event.isolated, send, listen, close });
    });
    window.addEventListener("message", onMessage);
    frame.addEventListener("load", () => send({ v: 1, type: "hello" }));
    document.body.append(frame);
  });
}

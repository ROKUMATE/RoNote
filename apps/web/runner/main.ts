import { parseRequest, type RunnerEvent } from "../src/runner/protocol";
import { createSession } from "./session";

/*
 * The runner page: served from its own origin and framed by the app, it runs
 * code from documents away from the app's data
 * (docs/adr/0008-in-browser-code-execution.md). The app names its origin in
 * the page's URL; the runner talks only to its parent, and only when that
 * origin is one of the app's.
 */

const app = new URLSearchParams(location.search).get("app") ?? "";

/** Sends message to the app, which the browser delivers only if the parent is on app's origin. */
function post(message: RunnerEvent) {
  window.parent.postMessage(message, app);
}

if (window.parent !== window && import.meta.env.RONOTE_APP_ORIGINS.includes(app)) {
  const python = createSession({
    spawn: () =>
      new Worker(new URL("./python.worker.ts", import.meta.url), {
        type: "module",
      }),
    interruptible: crossOriginIsolated,
    post,
  });
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent || event.origin !== app) return;
    const request = parseRequest(event.data);
    if (!request) return;
    switch (request.type) {
      case "hello":
        post({ v: 1, type: "hello", isolated: crossOriginIsolated });
        break;
      case "run":
        python.run(request);
        break;
      case "stop":
        python.stop();
        break;
      case "restart":
        python.restart();
        break;
    }
  });
}

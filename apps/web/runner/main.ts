import { configuredOrigin } from "../src/origins";
import { parseRequest, type RunnerEvent } from "../src/runner/protocol";
import { createSession } from "./session";

/*
 * The runner page: served from its own origin and framed by the app, it runs
 * code from documents away from the app's data
 * (docs/adr/0008-in-browser-code-execution.md), one session per runtime. The
 * app names its origin in the page's URL; the runner talks only to its
 * parent, and only when that origin is the app's, as the page's meta tag
 * names it.
 */

const app = new URLSearchParams(location.search).get("app") ?? "";

/** Sends message to the app, which the browser delivers only if the parent is on app's origin. */
function post(message: RunnerEvent) {
  window.parent.postMessage(message, app);
}

const appOrigin = configuredOrigin("ronote-app-url");

if (window.parent !== window && appOrigin !== "" && app === appOrigin) {
  const sessions = {
    python: createSession({
      runtime: "python",
      spawn: () =>
        new Worker(new URL("./python.worker.ts", import.meta.url), {
          type: "module",
        }),
      interruptible: crossOriginIsolated,
      post,
    }),
    // Stop ends a JavaScript session: nothing interrupts a busy JS worker.
    javascript: createSession({
      runtime: "javascript",
      spawn: () =>
        new Worker(new URL("./javascript.worker.ts", import.meta.url), {
          type: "module",
        }),
      interruptible: false,
      post,
    }),
  };
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent || event.origin !== app) return;
    const request = parseRequest(event.data);
    if (!request) return;
    switch (request.type) {
      case "hello":
        post({ v: 1, type: "hello", isolated: crossOriginIsolated });
        break;
      case "run":
        sessions[request.runtime].run(request);
        break;
      case "warm":
        sessions[request.runtime].warm();
        break;
      case "stop":
        sessions[request.runtime].stop();
        break;
      case "restart":
        sessions[request.runtime].restart();
        break;
    }
  });
}

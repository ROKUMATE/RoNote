# 0008: In-browser code execution

Accepted, 2026-11-05. Amended 2026-11-06: the production runner gets its own registrable domain. Amended in P13b: checking examples, JavaScript, unlabelled blocks, warming up Python.

## Context

From P13 on, readers run code blocks from their documents. That code comes from pages on the web: RoNote did not write it and cannot vouch for it. Run on the app's origin, it could read the reader's documents through the API, their cookies and storage, or the page itself.

## Decision

- **Code from docs is untrusted, and never runs on the app origin.**
- **A separate runner origin runs it** (dev `http://localhost:5174`; in production, P18, a separate registrable domain such as `ronote-run.dev`, never a subdomain of the app's).
  - It must never be the same site as the app. A subdomain is: `SameSite` cookies would go with every request the runner's code made to the app, and a cookie the app set with `Domain=` on its parent domain would be readable on the runner. Another registrable domain is another site, and gets neither.
  - The session cookie (P16) must be host-only: no `Domain` attribute.
  - In dev the two are different sites too: cookies ignore ports, so the runner is on `localhost` while the app is on `127.0.0.1`.
  - The app embeds the runner page in a hidden iframe with `sandbox="allow-scripts allow-same-origin"` and `allow="cross-origin-isolated"`.
  - The runner page runs each runtime in a Web Worker, so a busy interpreter never blocks either page.
  - `allow-same-origin` there means the runner's own origin: the runner can load its files and keep its worker, but it is still cross-origin to the app.
- **The two pages talk by `postMessage` only.**
  - The app names its origin in the runner page's URL (`?app=`). The runner talks only if that origin is one of the app's (`RONOTE_APP_ORIGINS`), and then sends every message to that origin only, so the browser delivers nothing to any other parent.
  - Each side checks `event.origin` against that one origin and `event.source` against the one window it expects: the runner its parent, the app its iframe.
  - The protocol is versioned (`v: 1`). Every message is validated against its shape (`src/runner/protocol.ts`, used by both sides).
  - Until a runner is deployed (P18), version 1 changes in place, as the app and the runner ship together; from then on, a change of shape bumps the version.
  - Anything with the wrong origin, source, version or shape is dropped without a reply.
- **The runner's headers:**
  - `Content-Security-Policy: default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; connect-src 'self'; frame-ancestors <the app's origins>; base-uri 'none'; form-action 'none'`;
  - `connect-src 'self'` keeps code from reaching the API or any other host; `'wasm-unsafe-eval'` is what compiling Pyodide's WebAssembly needs, and nothing else is allowed to evaluate;
  - one exception: the JavaScript worker's script (`javascript.worker.*`) is served with `script-src 'self' 'unsafe-eval'` in place of `'wasm-unsafe-eval'`, the rest the same, because running the reader's JavaScript is evaluating text. A dedicated worker lives by the policy its own script came with, not the page's, so the runner page and the Python worker still never may. The headers are chosen by path (`vite.runner.config.ts`), and P18's host must serve the same;
  - `frame-ancestors` lets only the app embed it;
  - `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Embedder-Policy: require-corp` and `Cross-Origin-Resource-Policy: cross-origin`.
- **The app is cross-origin isolated too,** with `Cross-Origin-Opener-Policy: same-origin` and `Cross-Origin-Embedder-Policy: credentialless`.
  - **Why:** a `SharedArrayBuffer` is what lets Stop interrupt Python while keeping its state (Pyodide's interrupt buffer), and an iframe gets one only when the top-level page is isolated.
  - **`credentialless`, not `require-corp`:** imported pages show images from their own sites, which don't send CORP headers; `credentialless` loads them without credentials instead of blocking them.
  - **Safari** supports no `credentialless`, so it isn't isolated. There, Stop ends the worker and starts a fresh session, and says the earlier state is gone.
  - COOP `same-origin` cuts the link to windows the app opens; sign-in (P16) must not depend on a popup's `window.opener`.
- **Python is Pyodide, self-hosted** from the runner origin: the WebAssembly runtime and Python's standard library, in the version the lockfile pins. No package loading, no micropip, no PyPI: code that imports anything else is told so.
- **JavaScript runs in a worker of its own** on the runner origin, in the browser's own engine, with no packages and no page:
  - a block runs as a script with top-level `await`. Before it runs, its top-level declarations (`let`, `const`, `var`, `class`, `function`) become assignments to names on the worker's global object, read with acorn, so later blocks see them and a block runs twice without "already declared", as in devtools' console. The block's text never changes, and positions in what ran map back to it;
  - a trailing expression's value is the run's value, as devtools shows one;
  - a run lasts as a Node script does: until its code has settled and the timers it set have fired or been cleared. A forgotten `setInterval` runs until Stop or the limit;
  - `console.log`, `info`, `debug`, `dir`, `warn` and `error` show as devtools would: objects two levels deep, cycles as `[Circular]`, Maps, Sets and errors by kind, `%s`-style specifiers;
  - an uncaught error or unhandled rejection is the run's error, its stack cut to the reader's frames;
  - nothing interrupts a busy JavaScript worker, so Stop, the time limit and the output limit end its session, and say the state is gone.
- **Sessions:**
  - one session per runtime per open document: blocks of a language share state, as cells of a notebook do;
  - each starts on its first Run, and Restart starts it again; the document's header shows where each runtime the document uses stands (not started, starting, idle, running), each with its own Restart;
  - Stop interrupts Python within a second; a run stops after 30 seconds, and output stops at 1 MB, for every runtime, kept by one limits module (`src/runner/limits.ts`);
  - a crash or running out of memory restarts the session, and says so.
  - The worker shares its realm with the code it runs, which can post messages as the worker would. So the runner page treats what its worker sends like a message from another origin (parsed, and about the run in progress, or dropped), and keeps the limits on its own clock.
  - `connect-src 'self'` is what keeps a request from being sent at all. COEP also refuses answers without a CORP header, but only after the request has gone.
- **Checking a transcript's examples** (`python_doctest` blocks, "Check examples", next to Run):
  - the runner passes the block's text, as is, to Python's own doctest, in the session's `__main__`, so a check sees what earlier blocks defined and keeps what it defines, as a run does;
  - doctest's checker decides each verdict; the runner sends each example's source, expected and actual output, and verdict, counted against the 1 MB output limit;
  - a traceback is shown as pages show one: its header and the exception;
  - the app labels two known false failures, so they never read as the reader's mistake: output that differs only in an object's address (`at 0x…>`, or without its `0x`, as some pages print one), and the same exception in other words, as Python's messages change between versions (the browser runs Python 3.14);
  - a failure shows a line diff of expected against actual, in the text's own colours.
- **What can run is derived, never stored.** A pure classifier reads each code block's language (the reader's choice over the page's) and text, and the names it shares with the document's other blocks, and names a class: `browser_python`, `python_doctest`, `browser_js`, `needs_packages`, `needs_network`, `needs_secret`, `needs_input`, `needs_dom`, `shell`, `fragment`, `unlabelled` or `unsupported`.
  - Each class has one plain sentence saying why.
  - JavaScript that uses a page's own names (`document`, `window`, `alert`, storage) is `needs_dom`; `fetch` and sockets are `needs_network`; a bare `import` or `require` is `needs_packages`, and a relative one or an `export` is a `fragment`. Strings and comments never count.
  - Only `browser_python`, `python_doctest` and `browser_js` show Run. `fragment` and `needs_packages` offer "Run anyway", since they may well run; the rest only explain.
  - A Python block that uses a name only blocks that can't run define can't run either, and takes the stricter of their classes; one that uses a name no block defines is a `fragment`. JavaScript blocks are read on their own.
  - RoNote still never guesses a language: a block with none is `unlabelled`, "This block has no language set. Choose one to run it.", and its chip opens the block's own language list.
- **Outputs are never saved.** They belong to the open page, and go when it closes.
- **Size:** the app's side of running (classifier, controls, output panel and the runner client) is its own lazy chunk with its own budget; Pyodide loads from the runner origin only when a reader first shows they mean to run Python: the first hover or focus of a Python Run or Check examples button starts the session (a `warm` request). Opening a page never does.

## Consequences

- Code from a page can compute and print, and nothing more: it can't read the reader's data, call the API, or reach the network.
- Code that needs packages, the network, secrets, input or a display is explained rather than run. Network and packages may come later, behind the same origin and policy.
- The JavaScript worker may evaluate code; what keeps that code in is what keeps Python in: its own origin, `connect-src 'self'`, and no page.
- Every deployment needs a second registrable domain for the runner, with its headers (P18).
- The app's own COOP and COEP apply to everything it serves from P13 on.

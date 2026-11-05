# 0008: In-browser code execution

Accepted, 2026-11-05

## Context

From P13 on, readers run code blocks from their documents. That code comes from pages on the web: RoNote did not write it and cannot vouch for it. Run on the app's origin, it could read the reader's documents through the API, their cookies and storage, or the page itself.

## Decision

- **Code from docs is untrusted, and never runs on the app origin.**
- **A separate runner origin runs it** (dev `http://localhost:5174`; a subdomain in production, P18).
  - Its cookies must never be the app's. Cookies ignore ports, so in dev the runner is on `localhost` while the app is on `127.0.0.1`; in production the app's cookies are host-only (no `Domain`, P16), so a subdomain never gets them.
  - The app embeds the runner page in a hidden iframe with `sandbox="allow-scripts allow-same-origin"` and `allow="cross-origin-isolated"`.
  - The runner page runs each runtime in a Web Worker, so a busy interpreter never blocks either page.
  - `allow-same-origin` there means the runner's own origin: the runner can load its files and keep its worker, but it is still cross-origin to the app.
- **The two pages talk by `postMessage` only.**
  - The app names its origin in the runner page's URL (`?app=`). The runner talks only if that origin is one of the app's (`RONOTE_APP_ORIGINS`), and then sends every message to that origin only, so the browser delivers nothing to any other parent.
  - Each side checks `event.origin` against that one origin and `event.source` against the one window it expects: the runner its parent, the app its iframe.
  - The protocol is versioned (`v: 1`). Every message is validated against its shape (`src/runner/protocol.ts`, used by both sides).
  - Anything with the wrong origin, source, version or shape is dropped without a reply.
- **The runner's headers:**
  - `Content-Security-Policy: default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; connect-src 'self'; frame-ancestors <the app's origins>; base-uri 'none'; form-action 'none'`;
  - `connect-src 'self'` keeps code from reaching the API or any other host; `'wasm-unsafe-eval'` is what compiling Pyodide's WebAssembly needs, and nothing else is allowed to evaluate;
  - `frame-ancestors` lets only the app embed it;
  - `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Embedder-Policy: require-corp` and `Cross-Origin-Resource-Policy: cross-origin`.
- **The app is cross-origin isolated too,** with `Cross-Origin-Opener-Policy: same-origin` and `Cross-Origin-Embedder-Policy: credentialless`.
  - **Why:** a `SharedArrayBuffer` is what lets Stop interrupt Python while keeping its state (Pyodide's interrupt buffer), and an iframe gets one only when the top-level page is isolated.
  - **`credentialless`, not `require-corp`:** imported pages show images from their own sites, which don't send CORP headers; `credentialless` loads them without credentials instead of blocking them.
  - **Safari** supports no `credentialless`, so it isn't isolated. There, Stop ends the worker and starts a fresh session, and says the earlier state is gone.
  - COOP `same-origin` cuts the link to windows the app opens; sign-in (P16) must not depend on a popup's `window.opener`.
- **Python is Pyodide, self-hosted** from the runner origin: the WebAssembly runtime and Python's standard library, in the version the lockfile pins. No package loading, no micropip, no PyPI: code that imports anything else is told so.
- **Sessions:**
  - one Python session per open document: blocks share state, as cells of a notebook do;
  - it starts on the first Run, and Restart starts it again;
  - Stop interrupts within a second; a run stops after 30 seconds, and output stops at 1 MB;
  - a crash or running out of memory restarts the session, and says so.
- **What can run is derived, never stored.** A pure classifier reads each code block's language (the reader's choice over the page's) and text, and the names it shares with the document's other blocks, and names a class: `browser_python`, `python_doctest`, `needs_packages`, `needs_network`, `needs_secret`, `needs_input`, `needs_dom`, `shell`, `fragment` or `unsupported`.
  - Each class has one plain sentence saying why.
  - Only `browser_python` and `python_doctest` show Run. `fragment` and `needs_packages` offer "Run anyway", since they may well run; the rest only explain.
  - A Python block that uses a name only blocks that can't run define can't run either, and takes the stricter of their classes; one that uses a name no block defines is a `fragment`.
  - RoNote still never guesses a language: an unlabelled block is `unsupported` until the reader chooses one.
- **Outputs are never saved.** They belong to the open page, and go when it closes.
- **Size:** the app's side of running (classifier, controls, output panel and the runner client) is its own lazy chunk with its own budget; Pyodide loads from the runner origin only when a reader first runs code.

## Consequences

- Code from a page can compute and print, and nothing more: it can't read the reader's data, call the API, or reach the network.
- Code that needs packages, the network, secrets, input or a display is explained rather than run. Network and packages may come later, behind the same origin and policy.
- Every deployment needs a second origin for the runner, with its headers (P18).
- The app's own COOP and COEP apply to everything it serves from P13 on.

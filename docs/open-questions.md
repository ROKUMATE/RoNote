# Open questions

Known gaps and behaviours that may surprise, left open on purpose. Remove an
entry when a phase settles it; add one when a phase leaves something open.

- go.dev declares no code languages. RoNote still never guesses one; the user can choose, per block or for all unlabelled code at once.
- Link status is resolved when a document opens and again after an import finishes. A page imported from another tab shows as not imported until then, and following it opens the existing document.
- Following a link that fails leaves its message under the link until the next link is followed.
- After macOS sleeps, the podman VM's clock can lag for a few minutes. Jobs no longer wait on it (P12), but tests that compare a database time with the app's, such as a user's `created_at` "about now", still fail meanwhile. `make doctor` says so and prints the fix.
- Code in a runner worker can post a false "done", which stops its run's clock while it keeps running. The next run then waits behind it and is interrupted at its own limit, so the waste lasts until the reader runs something or leaves.
- Safari isn't cross-origin isolated (no COEP `credentialless`), so Stop there ends the session; the e2e tests run Chromium only, so that path is covered by unit tests alone.
- Pyodide is 6.3 MB with gzip; a cold start took 0.75 s on fresh browsers here in P13b (P13a measured 4.2 s). The production runner (P18) should serve it compressed and cacheable.
- P18's host must serve the JavaScript worker's script (`javascript.worker.*`) with its own policy, `'unsafe-eval'` added, and every other file without it, as `headersFor` does.
- In dev, open the app at `127.0.0.1:5173`: the runner answers only the origins in `RONOTE_APP_ORIGINS`. At `localhost:5173` the app still loads, and a Run says the code runner didn't answer.
- JavaScript blocks are read on their own: one that uses a name another block imports (VitePress's `postcssIsolateStyles({...})`) offers Run, and fails with a `ReferenceError`.
- A JavaScript `setInterval` never cleared keeps its run going until Stop or the 30 s limit, as a Node script would. Output Python prints after its run has ended (from an asyncio task) is dropped.
- doctest ends an example's expected output at a blank line, so output a page shows with blank lines (the tutorial's `__doc__` example) fails without `<BLANKLINE>`. Version differences other than an exception's wording, such as Python 3.14's annotation order, are failures.
- Code blocks in documents wrap instead of scrolling sideways: Tiptap injects `.ProseMirror pre { white-space: pre-wrap }`, which wins over `.reader pre` (since P10; the styleguide's test checks the component outside the editor). Found in P13b, not fixed.
- The runner's Vite build warns that Pyodide's Node-only imports are left out; that is expected.
- Highlights go in prose the editor edits, and in code; not in table cells, captions, or prose inside tabs, which are drawn read-only.
- Jump to puts the caret at a highlight's start rather than selecting it, so the toolbar does not offer to highlight it again.
- A highlight's note is plain text; links and formatting in notes are for later.
- A user's document keeps the IR it was made from; re-importing returns it. Syncing to a newer snapshot will have to merge user blocks and edits, and DocContent keeps what that needs.
- A deleted source block can only come back by undo in the same session; there is no "restore from source" for missing blocks yet.
- Images inside source blocks are locked like code, tables and tabs, a step beyond the P10 brief, because the editor shows them as atoms.
- The "edited" label sits above the block, not in the notes margin, which annotations (P12+) will use.
- The fetch rate limit is per worker process; more than one worker process needs a shared limit.
- Pages of a recognised framework in another layout, such as a Docusaurus blog post, fall back to generic with `adapter_fallback`.
- A failed or lost import is shown until dismissed, but only in the session that started it.
- The URL length check counts characters in the browser and bytes on the server.

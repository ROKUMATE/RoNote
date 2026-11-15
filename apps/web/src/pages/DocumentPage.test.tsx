import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";
import type { IrWarning } from "../api/schema";
import { apiError, json, stubApi } from "../test/api";
import { renderApp } from "../test/render";

function documentWith(warnings?: IrWarning[]) {
  return {
    id: "doc-1",
    title: "Effective Go",
    source_url: "https://go.dev/doc/effective_go",
    version: 1,
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
    content: {
      doc_version: 1,
      blocks: [
        { id: "h", kind: "heading", level: 1, content: [{ text: "Effective Go" }] },
        { id: "c1", kind: "code", text: "x := 1\n" },
        { id: "l", kind: "list", items: [{ children: [{ id: "c2", kind: "code", text: "y\n" }] }] },
      ].map((b) => ({ ...b, origin: "source", edited: false, source_block_id: b.id })),
    },
    warnings: warnings ?? [],
  };
}

const notice = "Imported with the generic extractor; some page chrome may remain.";

async function openDocument(body: Response, state?: { notice: "existing" }) {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => body,
  });
  const app = renderApp("/");
  await app.router.navigate({
    to: "/doc/$id",
    params: { id: "doc-1" },
    ...(state ? { state } : {}),
  });
  return within(await screen.findByRole("main"));
}

test("shows the document's header", async () => {
  const main = await openDocument(json(documentWith()));

  expect(await main.findByRole("heading", { level: 1, name: "Effective Go" })).toBeDefined();
  const source = main.getByRole("link", { name: /go\.dev/ });
  expect(source.getAttribute("href")).toBe("https://go.dev/doc/effective_go");
  expect(source.getAttribute("target")).toBe("_blank");
  expect(source.getAttribute("rel")).toBe("noopener noreferrer");
  expect(
    main
      .getByText(/^Imported/)
      .querySelector("time")
      ?.getAttribute("dateTime"),
  ).toBe("2026-11-03T09:00:00Z");
  const counts = within(main.getByRole("list", { name: "Blocks" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent);
  expect(counts).toEqual(["1 heading", "1 list", "2 code blocks"]);
  expect(main.queryByRole("note")).toBeNull();
  expect(main.queryByText("Already in your library")).toBeNull();
});

test.each(["adapter_fallback", "low_confidence"] as const)(
  "warns that chrome may remain after %s",
  async (code) => {
    const main = await openDocument(json(documentWith([{ code, message: "x" }])));

    expect((await main.findByRole("note")).textContent).toBe(notice);
  },
);

test("says when an import found the page already in the library", async () => {
  const main = await openDocument(json(documentWith()), { notice: "existing" });

  const status = await main.findByText("Already in your library");
  expect(status.getAttribute("role")).toBe("status");
});

test("says when there is no such document", async () => {
  const main = await openDocument(apiError(404, "not_found"));

  expect(await main.findByText(/There's no document here\./)).toBeDefined();
  expect(main.getByRole("link", { name: "Go to the library" }).getAttribute("href")).toBe("/");
});

test("shows the title itself when the document does not open with it", async () => {
  const doc = documentWith();
  doc.content.blocks = doc.content.blocks.slice(1);
  const main = await openDocument(json(doc));

  expect(await main.findByRole("heading", { level: 1, name: "Effective Go" })).toBeDefined();
  expect(main.getAllByRole("heading", { level: 1 })).toHaveLength(1);
});

test("deletes the document after asking, then shows the library", async () => {
  let deleted = 0;
  const requests = stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => json(documentWith()),
    "DELETE /documents/doc-1": () => {
      deleted++;
      return new Response(null, { status: 204 });
    },
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  const main = within(await screen.findByRole("main"));

  fireEvent.click(await main.findByRole("button", { name: "Delete document" }));
  const dialog = await screen.findByRole("dialog", { name: "Delete this document?" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(deleted).toBe(0);

  fireEvent.click(main.getByRole("button", { name: "Delete document" }));
  fireEvent.click(
    within(await screen.findByRole("dialog", { name: "Delete this document?" })).getByRole(
      "button",
      { name: "Delete" },
    ),
  );

  await waitFor(() => expect(app.router.state.location.pathname).toBe("/"));
  expect(deleted).toBe(1);
  expect(requests.filter((r) => r.method === "DELETE")).toHaveLength(1);
});

test("says when a delete fails, and stays on the document", async () => {
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () => json(documentWith()),
    "DELETE /documents/doc-1": () => apiError(500, "internal_error"),
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  const main = within(await screen.findByRole("main"));

  fireEvent.click(await main.findByRole("button", { name: "Delete document" }));
  fireEvent.click(
    within(await screen.findByRole("dialog", { name: "Delete this document?" })).getByRole(
      "button",
      { name: "Delete" },
    ),
  );

  expect(await main.findByRole("alert")).toBeDefined();
  expect(app.router.state.location.pathname).toBe("/doc/doc-1");
});

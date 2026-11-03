import { screen, within } from "@testing-library/react";
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
      ir_version: 1,
      title: "Effective Go",
      source: {
        url: "https://go.dev/doc/effective_go",
        final_url: "https://go.dev/doc/effective_go",
        fetched_at: "2026-11-03T09:00:00Z",
        body_sha256: "a".repeat(64),
      },
      extractor: { name: "godev", version: "1" },
      blocks: [
        { id: "h", kind: "heading", level: 1, content: [{ text: "Effective Go" }] },
        { id: "c1", kind: "code", text: "x := 1\n" },
        { id: "l", kind: "list", items: [{ children: [{ id: "c2", kind: "code", text: "y\n" }] }] },
      ],
      ...(warnings ? { warnings } : {}),
    },
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

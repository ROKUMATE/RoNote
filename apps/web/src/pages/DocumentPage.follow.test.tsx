import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { beforeEach, expect, test, vi } from "vitest";
import { json, stubApi } from "../test/api";
import { send, subscriptions } from "../test/events";
import { renderApp } from "../test/render";

vi.mock("../api/events", () => import("../test/events"));

const inLibrary = "https://docs.example.com/b";
const notImported = "https://docs.example.com/c";

const doc = {
  id: "doc-a",
  title: "Guide A",
  source_url: "https://docs.example.com/a",
  version: 1,
  created_at: "2026-11-04T09:00:00Z",
  updated_at: "2026-11-04T09:00:00Z",
  warnings: [],
  content: {
    doc_version: 1,
    blocks: [
      {
        id: "p1",
        kind: "paragraph",
        content: [
          { text: "See " },
          { text: "part B", link: { href: `${inLibrary}#usage`, kind: "same_domain" } },
          { text: " and " },
          { text: "part C", link: { href: notImported, kind: "same_domain" } },
        ],
        origin: "source",
        edited: false,
        source_block_id: "p1",
      },
    ],
  },
};

const queued = {
  id: "imp-c",
  requested_url: notImported,
  canonical_url: notImported,
  status: "queued",
  created_at: "2026-11-04T09:00:00Z",
  updated_at: "2026-11-04T09:00:00Z",
};

beforeEach(() => {
  subscriptions.length = 0;
});

async function openDoc(importResponse = () => json({ import: queued }, 202)) {
  const requests = stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-a": () => json(doc),
    "GET /documents/doc-b": () => json({ ...doc, id: "doc-b", title: "Guide B" }),
    "GET /documents/doc-c": () => json({ ...doc, id: "doc-c", title: "Guide C" }),
    "POST /documents/resolve": () =>
      json({ links: [{ url: `${inLibrary}#usage`, document_id: "doc-b" }, { url: notImported }] }),
    "POST /imports": importResponse,
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-a" } });
  const main = within(await screen.findByRole("main"));
  await main.findByText("part B");
  await waitFor(() =>
    expect(
      main.getByRole("link", { name: "part B" }).querySelector('[data-link="internal"]'),
    ).not.toBeNull(),
  );
  const editor = (document.querySelector(".ProseMirror") as HTMLElement & { editor: Editor })
    .editor;
  return { ...app, main, requests, editor };
}

test("a link to a page in the library opens it, at the link's section", async () => {
  const { main, router } = await openDoc();

  fireEvent.click(main.getByRole("link", { name: "part B" }));

  await waitFor(() => expect(router.state.location.pathname).toBe("/doc/doc-b"));
  expect(router.state.location.hash).toBe("usage");
});

test("a link to a page not imported yet imports it, shows its steps, then opens it", async () => {
  const { main, router, requests } = await openDoc();

  fireEvent.click(main.getByRole("link", { name: "part C" }));

  expect(await main.findByText("Queued…", {}, { timeout: 2000 })).toBeDefined();
  const posted = requests.find((r) => r.method === "POST" && r.url.endsWith("/imports"));
  expect(await posted?.json()).toEqual({ url: notImported });
  send({ status: "fetching" });
  expect(main.getByText("Fetching the page…")).toBeDefined();
  send({ status: "done", document_id: "doc-c" });

  await waitFor(() => expect(router.state.location.pathname).toBe("/doc/doc-c"));
});

test("a page that cannot be imported says why, and offers the original", async () => {
  const { main } = await openDoc();

  fireEvent.click(main.getByRole("link", { name: "part C" }));
  await main.findByText("Queued…");
  send({ status: "failed", error_code: "unsupported_type" });

  const alert = main.getByRole("alert");
  expect(alert.textContent).toContain("That link isn't a web page. PDF import comes later.");
  const original = within(alert).getByRole("link", { name: "Open original" });
  expect([
    original.getAttribute("href"),
    original.getAttribute("target"),
    original.getAttribute("rel"),
  ]).toEqual([notImported, "_blank", "noopener noreferrer"]);
});

test("Cmd-click and middle-click open the original instead", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const { main, router, requests } = await openDoc();
  const link = main.getByRole("link", { name: "part C" });

  fireEvent.click(link, { metaKey: true });
  fireEvent(link, new MouseEvent("auxclick", { bubbles: true, button: 1 }));

  expect(open.mock.calls).toEqual([
    [notImported, "_blank", "noopener,noreferrer"],
    [notImported, "_blank", "noopener,noreferrer"],
  ]);
  expect(requests.filter((r) => r.url.endsWith("/imports"))).toEqual([]);
  expect(router.state.location.pathname).toBe("/doc/doc-a");
});

test("pressing a link does not move the caret; its text is still editable from the keyboard", async () => {
  const { main, editor } = await openDoc();
  act(() => {
    editor.commands.setTextSelection(2);
  });
  const link = main.getByRole("link", { name: "part C" });

  expect(fireEvent.mouseDown(link, { button: 0 })).toBe(false);
  expect(editor.state.selection.from).toBe(2);

  // From the keyboard: into the link's text ("part C" spans 16-22), then type.
  act(() => {
    editor.chain().setTextSelection(21).insertContent("big ").run();
  });
  expect(main.getByRole("link", { name: "part big C" }).getAttribute("href")).toBe(notImported);
});

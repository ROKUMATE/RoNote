import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { documentsKey } from "../api/queries";
import { apiError, json, stubApi } from "../test/api";
import { send, subscriptions } from "../test/events";
import { renderApp } from "../test/render";

vi.mock("../api/events", () => import("../test/events"));

const queued = {
  id: "imp-1",
  requested_url: "https://docs.example.com/guide",
  canonical_url: "https://docs.example.com/guide",
  status: "queued",
  created_at: "2026-11-03T09:00:00Z",
  updated_at: "2026-11-03T09:00:00Z",
};

const summary = {
  id: "doc-1",
  title: "Guide",
  source_url: "https://docs.example.com/guide",
  version: 1,
  created_at: "2026-11-03T09:00:00Z",
  updated_at: "2026-11-03T09:00:00Z",
};

const documentWithContent = {
  ...summary,
  content: {
    ir_version: 1,
    title: "Guide",
    source: {
      url: summary.source_url,
      final_url: summary.source_url,
      fetched_at: "2026-11-03T09:00:00Z",
      body_sha256: "a".repeat(64),
    },
    extractor: { name: "generic", version: "2" },
    blocks: [],
  },
};

beforeEach(() => {
  subscriptions.length = 0;
});

async function openDialog() {
  const app = renderApp("/");
  fireEvent.click(await screen.findByRole("button", { name: "Import page" }));
  const dialog = screen.getByRole("dialog", { name: "Import page" });
  expect(dialog).toHaveProperty("open", true);
  return { ...app, dialog };
}

function submit(dialog: HTMLElement, url: string) {
  fireEvent.change(within(dialog).getByLabelText("Page URL"), { target: { value: url } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Import" }));
}

function progress(dialog: HTMLElement) {
  return within(dialog)
    .getAllByRole("listitem")
    .map((item) => `${item.dataset.step}:${item.dataset.state}`);
}

describe("a new import", () => {
  test("shows its steps in order, then opens the document", async () => {
    stubApi({
      "POST /imports": () => json({ import: queued }, 202),
      "GET /documents": () => json({ documents: [] }),
      "GET /documents/doc-1": () => json(documentWithContent),
    });
    const { dialog, queryClient, router } = await openDialog();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    submit(dialog, "  https://docs.example.com/guide ");

    await within(dialog).findByRole("list", { name: "Progress" });
    expect(subscriptions.map((s) => s.id)).toEqual(["imp-1"]);
    expect(progress(dialog)).toEqual([
      "queued:current",
      "fetching:waiting",
      "extracting:waiting",
      "done:waiting",
    ]);
    send({ status: "queued" });
    send({ status: "fetching" });
    expect(progress(dialog)).toEqual([
      "queued:complete",
      "fetching:current",
      "extracting:waiting",
      "done:waiting",
    ]);
    expect(within(dialog).getByRole("status").textContent).toContain("Fetching the page");
    send({ status: "extracting" });
    expect(progress(dialog)).toEqual([
      "queued:complete",
      "fetching:complete",
      "extracting:current",
      "done:waiting",
    ]);
    expect(invalidate).not.toHaveBeenCalled();

    send({ status: "done", document_id: "doc-1" });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: documentsKey });
    await waitFor(() => expect(router.state.location.pathname).toBe("/doc/doc-1"));
    expect(dialog).toHaveProperty("open", false);
    expect(await screen.findByRole("heading", { level: 1, name: "Guide" })).toBeDefined();
    expect(screen.queryByText("Already in your library")).toBeNull();
  });

  test("keeps going after the dialog is closed", async () => {
    stubApi({
      "POST /imports": () => json({ import: queued }, 202),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog, queryClient, router } = await openDialog();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    submit(dialog, "https://docs.example.com/guide");
    await within(dialog).findByRole("list", { name: "Progress" });

    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(dialog).toHaveProperty("open", false);
    expect(subscriptions[0]?.unsubscribe).not.toHaveBeenCalled();

    send({ status: "done", document_id: "doc-1" });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: documentsKey });
    expect(subscriptions[0]?.unsubscribe).toHaveBeenCalled();
    expect(router.state.location.pathname).toBe("/");
  });

  test.each([
    ["not_found", "The site says that page doesn't exist."],
    ["disallowed_by_robots", "This site doesn't allow RoNote to fetch its pages."],
    ["unsupported_type", "That link isn't a web page. PDF import comes later."],
  ] as const)("that fails with %s says why", async (code, sentence) => {
    stubApi({
      "POST /imports": () => json({ import: queued }, 202),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog, tracker } = await openDialog();
    submit(dialog, "https://docs.example.com/guide");
    await within(dialog).findByRole("list", { name: "Progress" });

    send({ status: "failed", error_code: code });

    expect(within(dialog).getByRole("alert").textContent).toContain(sentence);
    fireEvent.click(within(dialog).getByRole("button", { name: "Try another link" }));
    expect(within(dialog).getByLabelText("Page URL")).toHaveProperty("value", "");
    expect(tracker.getSnapshot()).toEqual([]);
  });
});

test("a page already in the library opens its document with a notice", async () => {
  stubApi({
    "POST /imports": () =>
      json({ import: { ...queued, status: "done", document_id: "doc-1" }, document: summary }),
    "GET /documents": () => json({ documents: [summary] }),
    "GET /documents/doc-1": () => json(documentWithContent),
  });
  const { dialog, router } = await openDialog();

  submit(dialog, "https://docs.example.com/guide");

  await waitFor(() => expect(router.state.location.pathname).toBe("/doc/doc-1"));
  expect(dialog).toHaveProperty("open", false);
  expect((await screen.findByRole("status")).textContent).toContain("Already in your library");
  expect(subscriptions).toEqual([]);
});

describe("the URL field", () => {
  test.each([
    ["ftp://docs.example.com/", "Only http and https links can be imported."],
    ["not a url", "That isn't a web address. Paste a link that starts with https://."],
    ["", "That isn't a web address. Paste a link that starts with https://."],
    [`https://docs.example.com/${"a".repeat(2049)}`, "That link is longer than 2048 characters."],
  ])("refuses %j without asking the API", async (url, sentence) => {
    const requests = stubApi({ "GET /documents": () => json({ documents: [] }) });
    const { dialog } = await openDialog();

    submit(dialog, url);

    const field = within(dialog).getByLabelText("Page URL");
    const description = document.getElementById(field.getAttribute("aria-describedby") ?? "");
    expect(description?.textContent).toBe(sentence);
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(requests.filter((r) => r.method === "POST")).toEqual([]);
  });

  test("shows the API's reason when it refuses the URL", async () => {
    stubApi({
      "POST /imports": () => apiError(400, "ip_literal_host"),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog } = await openDialog();

    submit(dialog, "http://10.0.0.1/docs");

    expect(
      await within(dialog).findByText("Use the site's name, not an IP address."),
    ).toBeDefined();
  });

  test("says so when the API does not answer", async () => {
    stubApi({
      "POST /imports": () => new Response("Bad Gateway", { status: 502 }),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog } = await openDialog();

    submit(dialog, "https://docs.example.com/guide");

    expect(
      await within(dialog).findByText("RoNote's server isn't answering. Try again in a moment."),
    ).toBeDefined();
  });
});

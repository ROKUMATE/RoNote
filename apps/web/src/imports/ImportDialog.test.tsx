import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { documentsKey } from "../api/queries";
import { apiError, json, stubApi } from "../test/api";
import { send, subscriptions } from "../test/events";
import { renderApp } from "../test/render";

vi.mock("../api/events", () => import("../test/events"));

const queued = {
  id: "imp-1",
  kind: "web",
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
  content: { doc_version: 1, blocks: [] },
  warnings: [],
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
    // The document route is a lazy chunk (src/router.tsx): the first import
    // of the reader and editor is slow while cold, and a findBy below would
    // wait for it inside one second. Await it here, where it is the point.
    await import("../pages/DocumentPage");

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
    // The document is on screen once its title is; asking for its role
    // is a second, slow query, so ask once, not in findBy's one-second poll.
    await screen.findByText("Guide");
    expect(screen.getByRole("heading", { level: 1, name: "Guide" })).toBeDefined();
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
    ["unsupported_type", "That link isn't a web page; to import a PDF, upload the file."],
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
  const notice = await screen.findByText("Already in your library");
  expect(notice.getAttribute("role")).toBe("status");
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

describe("a PDF upload", () => {
  const pdfQueued = {
    id: "imp-2",
    kind: "pdf",
    filename: "Lecture 3.pdf",
    status: "queued",
    created_at: "2026-11-03T09:00:00Z",
    updated_at: "2026-11-03T09:00:00Z",
  };

  function pdf(name = "Lecture 3.pdf", type = "application/pdf", size = 9) {
    const file = new File(["%PDF-1.7"], name, { type });
    if (size !== 9) Object.defineProperty(file, "size", { value: size });
    return file;
  }

  async function openUpload() {
    const opened = await openDialog();
    // Radix Tabs switch on the mouse button going down, as a click starts.
    fireEvent.mouseDown(within(opened.dialog).getByRole("tab", { name: "Upload PDF" }));
    return opened;
  }

  test("sends the file and shows a PDF's steps", async () => {
    const requests = stubApi({
      "POST /imports/pdf": () => json({ import: pdfQueued }, 202),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog } = await openUpload();

    fireEvent.change(within(dialog).getByLabelText("Choose a PDF"), { target: { files: [pdf()] } });
    expect(within(dialog).getByText(/Lecture 3\.pdf/)).toBeDefined();
    fireEvent.click(within(dialog).getByRole("button", { name: "Upload" }));

    await within(dialog).findByRole("list", { name: "Progress" });
    const sent = requests.find((r) => r.method === "POST");
    // jsdom's File isn't Node's, so the part's content is checked end to
    // end (e2e/pdf.spec.ts); here, that the form has its one file part.
    expect(sent?.headers.get("content-type")).toMatch(/^multipart\/form-data; boundary=/);
    expect(await sent?.text()).toContain('name="file"');
    expect(progress(dialog)).toEqual([
      "queued:current",
      "converting:waiting",
      "extracting:waiting",
      "done:waiting",
    ]);
    send({ status: "converting" });
    expect(await within(dialog).findByRole("status")).toHaveProperty(
      "textContent",
      "Converting the PDF",
    );
  });

  test("takes a file dropped on it", async () => {
    stubApi({ "GET /documents": () => json({ documents: [] }) });
    const { dialog } = await openUpload();

    fireEvent.drop(within(dialog).getByTestId("pdf-drop"), {
      dataTransfer: { files: [pdf("Dropped.pdf")] },
    });

    expect(within(dialog).getByText(/Dropped\.pdf/)).toBeDefined();
  });

  test.each([
    ["a file that isn't a PDF", pdf("notes.txt", "text/plain"), "That file isn't a PDF."],
    [
      "a PDF over 25 MB",
      pdf("big.pdf", "application/pdf", 25 * 1024 * 1024 + 1),
      "That PDF is larger than 25 MB, the most RoNote imports.",
    ],
  ])("refuses %s before sending it", async (_, file, sentence) => {
    const requests = stubApi({ "GET /documents": () => json({ documents: [] }) });
    const { dialog } = await openUpload();

    fireEvent.change(within(dialog).getByLabelText("Choose a PDF"), { target: { files: [file] } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Upload" }));

    expect(await within(dialog).findByText(sentence)).toBeDefined();
    expect(requests.filter((r) => r.method === "POST")).toHaveLength(0);
  });

  test("says why the server refused it", async () => {
    stubApi({
      "POST /imports/pdf": () => apiError(400, "not_a_pdf"),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog } = await openUpload();

    fireEvent.change(within(dialog).getByLabelText("Choose a PDF"), { target: { files: [pdf()] } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Upload" }));

    expect(await within(dialog).findByText("That file isn't a PDF.")).toBeDefined();
  });

  test("a scan says why it can't be imported", async () => {
    stubApi({
      "POST /imports/pdf": () => json({ import: pdfQueued }, 202),
      "GET /documents": () => json({ documents: [] }),
    });
    const { dialog } = await openUpload();
    fireEvent.change(within(dialog).getByLabelText("Choose a PDF"), { target: { files: [pdf()] } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Upload" }));
    await within(dialog).findByRole("list", { name: "Progress" });

    send({ status: "failed", error_code: "scanned_pdf_unsupported" });

    expect((await within(dialog).findByRole("alert")).textContent).toBe(
      "That PDF is scanned pages without text, which RoNote can't read yet.",
    );
    expect(within(dialog).getByRole("button", { name: "Try another file" })).toBeDefined();
  });
});

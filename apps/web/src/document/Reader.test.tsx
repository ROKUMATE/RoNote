import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { DocBlock, DocContent, IrBlock } from "../api/schema";
import { Reader } from "./Reader";

function source(blocks: IrBlock[]): DocContent {
  return {
    doc_version: 1,
    blocks: blocks.map(
      (b): DocBlock => ({ ...b, origin: "source", edited: false, source_block_id: b.id }),
    ),
  };
}

const code: IrBlock = {
  id: "c1",
  kind: "code",
  lang: "yaml",
  title: "pods/simple-pod.yaml",
  title_link: { href: "https://raw.example.com/pods/simple-pod.yaml", kind: "external" },
  text: "apiVersion: v1\nkind: Pod\n",
};

const tabs: IrBlock = {
  id: "t1",
  kind: "tabs",
  items: [
    { label: "npm", children: [{ id: "c2", kind: "code", text: "npm install\n" }] },
    { label: "pnpm", children: [{ id: "c3", kind: "code", text: "pnpm add\n" }] },
  ],
};

let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  vi.restoreAllMocks();
});

test("a code block shows its title link and language, and Copy copies its exact text", async () => {
  render(<Reader content={source([code])} />);

  const title = await screen.findByRole("link", { name: "pods/simple-pod.yaml" });
  expect(title.getAttribute("href")).toBe("https://raw.example.com/pods/simple-pod.yaml");
  expect(title.getAttribute("target")).toBe("_blank");
  expect(title.getAttribute("rel")).toBe("noopener noreferrer");
  expect(screen.getByText("yaml")).toBeDefined();
  const pre = document.querySelector("pre");
  expect(pre?.textContent).toBe(code.text);
  expect(pre?.getAttribute("tabindex")).toBe("0");

  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }));
  });

  expect(writeText).toHaveBeenCalledWith(code.text);
  expect(screen.getByRole("button", { name: "Copied code" })).toBeDefined();
});

test("tabs follow the tabs pattern: arrow keys move between them", async () => {
  render(<Reader content={source([tabs])} />);

  const list = await screen.findByRole("tablist");
  const [npm, pnpm] = within(list).getAllByRole("tab");
  if (!npm || !pnpm) throw new Error("two tabs expected");
  expect(npm.getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("tabpanel").textContent).toContain("npm install");

  act(() => npm.focus());
  fireEvent.keyDown(npm, { key: "ArrowRight" });

  await waitFor(() => expect(pnpm.getAttribute("aria-selected")).toBe("true"));
  expect(document.activeElement).toBe(pnpm);
  expect(screen.getByRole("tabpanel").textContent).toContain("pnpm add");
});

test("an image loads lazily, without a referrer, with its alt text", async () => {
  render(
    <Reader
      content={source([
        { id: "i1", kind: "image", src: "https://docs.example.com/a.png", alt: "A diagram" },
      ])}
    />,
  );

  const img = await screen.findByRole("img", { name: "A diagram" });
  expect(img.getAttribute("loading")).toBe("lazy");
  expect(img.getAttribute("referrerpolicy")).toBe("no-referrer");
});

test("text is editable; code, tables, tabs and images are not", async () => {
  render(
    <Reader
      content={source([
        { id: "p1", kind: "paragraph", content: [{ text: "Editable." }] },
        code,
        { id: "tb", kind: "table", rows: [[[{ text: "cell" }]]] },
        tabs,
      ])}
    />,
  );

  const editor = await screen.findByLabelText("Document text");
  expect(editor.getAttribute("contenteditable")).toBe("true");
  expect(within(editor).getByText("Editable.").closest('[contenteditable="false"]')).toBeNull();
  for (const locked of [
    document.querySelector("pre"),
    screen.getByText("cell"),
    screen.getByRole("tablist"),
  ]) {
    expect(locked?.closest('[contenteditable="false"]')).not.toBeNull();
  }
});

test("links: other pages open in a new tab, anchors move the caret within the document", async () => {
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  render(
    <Reader
      content={source([
        { id: "h1", kind: "heading", level: 2, anchor: "setup", content: [{ text: "Setup" }] },
        {
          id: "p1",
          kind: "paragraph",
          content: [
            { text: "See " },
            { text: "setup", link: { href: "#setup", kind: "anchor" } },
            { text: " or " },
            { text: "the spec", link: { href: "https://spec.example.com/", kind: "external" } },
          ],
        },
      ])}
    />,
  );

  const external = await screen.findByRole("link", { name: "the spec" });
  expect(external.getAttribute("target")).toBe("_blank");
  expect(external.getAttribute("rel")).toBe("noopener noreferrer");
  fireEvent.click(external);
  expect(open).toHaveBeenCalledWith("https://spec.example.com/", "_blank", "noopener,noreferrer");

  const anchor = screen.getByRole("link", { name: "setup" });
  expect(anchor.getAttribute("target")).toBeNull();
  fireEvent.click(anchor);
  const heading = screen.getByRole("heading", { name: "Setup" });
  expect(heading.scrollIntoView).toHaveBeenCalled();
  expect(document.activeElement).toBe(screen.getByLabelText("Document text"));
  expect(window.getSelection()?.anchorNode?.parentElement?.closest("h2")).toBe(heading);
});

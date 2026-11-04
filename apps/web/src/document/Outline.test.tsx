import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { expect, test, vi } from "vitest";
import type { DocBlock, IrBlock } from "../api/schema";
import { outlineSlotId } from "../shell/AppShell";
import { Outline } from "./Outline";
import { Reader } from "./Reader";

const source = (b: IrBlock): DocBlock => ({
  ...b,
  origin: "source",
  edited: false,
  source_block_id: b.id,
});
const heading = (id: string, level: number, text: string, anchor?: string) =>
  source({ id, kind: "heading", level, content: [{ text }], ...(anchor ? { anchor } : {}) });

async function openOutline() {
  let editor: Editor | undefined;
  const blocks = [
    heading("h1", 1, "Guide"),
    heading("h2a", 2, "Install", "install"),
    source({ id: "p1", kind: "paragraph", content: [{ text: "Text." }] }),
    heading("h3", 3, "On macOS", "on-macos"),
    heading("h2b", 2, "Use", "use"),
    heading("h4", 4, "Too deep"),
  ];
  const { rerender } = render(
    <>
      <div id={outlineSlotId} />
      <Reader content={{ doc_version: 1, blocks }} onEditor={(e) => (editor = e)} />
    </>,
  );
  await screen.findByText("Text.");
  if (!editor) throw new Error("no editor");
  const ready = editor;
  rerender(
    <>
      <div id={outlineSlotId} />
      <Reader content={{ doc_version: 1, blocks }} onEditor={(e) => (editor = e)} />
      <Outline editor={ready} />
    </>,
  );
  return {
    editor: ready,
    outline: within(await screen.findByRole("navigation", { name: "Outline" })),
  };
}

test("lists the h2 and h3 headings, in the sidebar slot", async () => {
  const { outline } = await openOutline();

  const links = outline.getAllByRole("link");
  expect(links.map((l) => l.textContent)).toEqual(["Install", "On macOS", "Use"]);
  expect(links.map((l) => l.getAttribute("href"))).toEqual(["#install", "#on-macos", "#use"]);
  expect(
    screen.getByRole("navigation", { name: "Outline" }).closest(`#${outlineSlotId}`),
  ).not.toBeNull();
});

test("clicking a heading scrolls to it and puts the caret there", async () => {
  const { editor, outline } = await openOutline();
  Element.prototype.scrollIntoView = vi.fn();

  fireEvent.click(outline.getByRole("link", { name: "Use" }));

  const target = screen.getByRole("heading", { name: "Use" });
  expect(target.scrollIntoView).toHaveBeenCalled();
  expect(document.activeElement).toBe(editor.view.dom);
  expect(editor.state.selection.$from.parent.textContent).toBe("Use");
  expect(outline.getByRole("link", { name: "Use" }).getAttribute("aria-current")).toBe("location");
});

test("follows headings as they are edited", async () => {
  const { editor, outline } = await openOutline();

  act(() => {
    // "Guide" takes positions 0-6; "Install" ends at 15.
    editor.chain().setTextSelection(15).insertContent(" it").run();
  });

  expect(outline.getAllByRole("link")[0]?.textContent).toBe("Install it");
});

test("knows the current heading after a jump that skipped past others", async () => {
  const { outline } = await openOutline();
  const tops: Record<string, number> = { Install: -1200, "On macOS": 120, Use: 900 };
  for (const [name, top] of Object.entries(tops)) {
    const heading = screen.getByRole("heading", { name });
    heading.getBoundingClientRect = () => ({ top, bottom: top + 30 }) as DOMRect;
  }

  // No heading entered the reading zone on the way: the page jumped.
  act(() => {
    window.dispatchEvent(new Event("scroll"));
  });
  await act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

  expect(outline.getByRole("link", { name: "On macOS" }).getAttribute("aria-current")).toBe(
    "location",
  );
});

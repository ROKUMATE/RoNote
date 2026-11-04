import { act, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import type { DocContent } from "../api/schema";
import { bringIntoView } from "../test/intersection";
import { Reader } from "./Reader";

const highlight = vi.hoisted(() =>
  vi.fn(async (code: string, _language: string) => [
    code
      .split("\n")
      .slice(0, 1)
      .map((content) => ({ content, color: "var(--code-token-keyword)", offset: 0 })),
    ...code
      .split("\n")
      .slice(1)
      .map((content) => [{ content, color: "var(--code-foreground)", offset: 0 }]),
  ]),
);
vi.mock("./highlighter", () => ({ highlight }));

const content: DocContent = {
  doc_version: 1,
  blocks: [
    {
      id: "c1",
      kind: "code",
      lang: "go",
      text: "package main\nfunc main() {}\n",
      origin: "source",
      edited: false,
      source_block_id: "c1",
    },
    {
      id: "c2",
      kind: "code",
      lang: "nope",
      text: "plain\n",
      origin: "source",
      edited: false,
      source_block_id: "c2",
    },
  ],
};

test("code is highlighted only once it nears the viewport, and keeps its text", async () => {
  render(<Reader content={content} />);
  await vi.waitFor(() =>
    expect(screen.getAllByRole("button", { name: "Copy code" })).toHaveLength(2),
  );
  const [goBlock, plainBlock] = screen
    .getAllByRole("button", { name: "Copy code" })
    .map((b) => b.closest(".code-block"));
  if (!goBlock || !plainBlock) throw new Error("two code blocks expected");

  await act(async () => {});
  expect(highlight).not.toHaveBeenCalled();

  await act(async () => {
    bringIntoView(goBlock, plainBlock);
  });
  await vi.waitFor(() => expect(goBlock.querySelector("pre span")).not.toBeNull());

  expect(highlight).toHaveBeenCalledTimes(1);
  expect(highlight.mock.calls[0]?.[1]).toBe("go");
  expect(goBlock.querySelector("pre")?.textContent).toBe("package main\nfunc main() {}\n");
  expect(goBlock.querySelector("pre span")?.getAttribute("style")).toBe(
    "color: var(--code-token-keyword);",
  );
  expect(plainBlock.querySelector("pre span")).toBeNull();
});

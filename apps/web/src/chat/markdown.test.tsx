import { createMemoryHistory } from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import type { Citation, MessageSegment } from "../api/schema";
import { blocks, Markdown, withMarkers } from "./markdown";

const doc = "0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10";
const other = "0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e99";
const channels: Citation = {
  n: 1,
  document_id: doc,
  block_ids: ["b1", "b2"],
  cited_text: "Channels let goroutines communicate.",
  title: "Effective Go > Channels",
};

// Markdown links lead into the app's router, which these tests stand up
// in memory around the answer.
async function renderAnswer(segments: MessageSegment[], citations: Citation[] = [channels]) {
  const { createRootRoute, createRoute, createRouter, RouterProvider } = await import(
    "@tanstack/react-router"
  );
  const onLand = vi.fn();
  const root = createRootRoute({
    component: () => <Markdown segments={segments} citations={citations} onLand={onLand} />,
  });
  const docRoute = createRoute({ getParentRoute: () => root, path: "/doc/$id" });
  const router = createRouter({
    routeTree: root.addChildren([docRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const result = render(<RouterProvider router={router} />);
  await screen.findByRole("button", { name: /^Source 1/ }).catch(() => undefined);
  return { ...result, onLand };
}

test("a citation's number follows its segment's words, before the space after them", () => {
  expect(
    withMarkers([
      { text: "Channels connect goroutines. ", citations: [1, 2] },
      { text: "That's all.", citations: [] },
    ]),
  ).toBe("Channels connect goroutines.12 That's all.");
});

test("blocks: paragraphs, lists, code; headings and quotes as text", () => {
  expect(
    blocks(
      "# Title\n\nOne line\nand the next.\n\n- a\n- b\n  more of b\n1. first\n\n```go\nx := 1\n\n```\n> quoted",
    ),
  ).toEqual([
    { kind: "p", text: "Title" },
    { kind: "p", text: "One line and the next." },
    { kind: "list", ordered: false, items: ["a", "b more of b"] },
    { kind: "list", ordered: true, items: ["first"] },
    { kind: "code", lang: "go", text: "x := 1\n" },
    { kind: "p", text: "quoted" },
  ]);
});

test("images, raw HTML and links elsewhere render as text; only cited documents link", async () => {
  const { container } = await renderAnswer([
    {
      text:
        "See ![the diagram](https://evil.example/x.png) and <img src=x onerror=alert(1)> <b>bold?</b>. " +
        "Read [the docs](https://evil.example/phish), [your page](/doc/" +
        doc +
        "?block=b1) and [another](/doc/" +
        other +
        ").",
      citations: [1],
    },
  ]);
  expect(container.querySelector("img")).toBeNull();
  expect(container.querySelector("b")).toBeNull();
  expect(container.textContent).toContain(
    "See the diagram and <img src=x onerror=alert(1)> <b>bold?</b>.",
  );
  const links = within(container).getAllByRole("link");
  expect(links.map((a) => a.getAttribute("href"))).toEqual([`/doc/${doc}?block=b1`]);
  expect(links[0]?.textContent).toBe("your page");
  expect(container.textContent).toContain("Read the docs, your page and another.");
});

test("inline code, bold and italics; identifiers keep their underscores", async () => {
  const { container } = await renderAnswer([
    {
      text: "Use `make(chan int)` with **care** and *thought*; snake_case_names stay.",
      citations: [],
    },
  ]);
  expect(container.querySelector("code")?.textContent).toBe("make(chan int)");
  expect(container.querySelector("strong")?.textContent).toBe("care");
  expect(container.querySelector("em")?.textContent).toBe("thought");
  expect(container.textContent).toContain("snake_case_names stay.");
});

test("a chip shows what it cites on focus, and lands on its block when clicked", async () => {
  const { onLand } = await renderAnswer([{ text: "Goroutines use channels.", citations: [1] }]);
  const chip = screen.getByRole("button", { name: "Source 1: Effective Go > Channels" });
  const description = document.getElementById(chip.getAttribute("aria-describedby") ?? "");
  expect(description?.textContent).toBe("Channels let goroutines communicate.");
  expect(screen.queryByText("Effective Go > Channels")).toBeNull();
  chip.focus();
  await screen.findByText("Effective Go > Channels");
  chip.click();
  expect(onLand).toHaveBeenCalledWith(channels);
});

test("a number with no citation behind it renders nothing", async () => {
  await renderAnswer([{ text: "Made up.", citations: [7] }], []);
  expect(screen.queryByRole("button")).toBeNull();
  screen.getByText("Made up.");
});

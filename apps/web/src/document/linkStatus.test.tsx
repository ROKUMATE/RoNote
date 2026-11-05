import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import type { DocBlock, IrBlock, IrLink } from "../api/schema";
import { followHelpId } from "./links";
import { Reader } from "./Reader";

const source = (b: IrBlock): DocBlock => ({
  ...b,
  origin: "source",
  edited: false,
  source_block_id: b.id,
});
const link = (text: string, href: string, kind: IrLink["kind"]) => ({
  text,
  link: { href, kind },
});

const blocks = [
  source({
    id: "p1",
    kind: "paragraph",
    content: [
      link("in the library", "https://d.example/a", "same_domain"),
      { text: " and " },
      link("not yet", "https://d.example/b#usage", "same_domain"),
      { text: " and " },
      link("elsewhere", "https://o.example/", "external"),
    ],
  }),
  source({
    id: "t1",
    kind: "tabs",
    items: [
      {
        label: "One",
        children: [
          {
            id: "tp",
            kind: "paragraph",
            content: [link("in a tab", "https://d.example/b#usage", "same_domain")],
          },
        ],
      },
    ],
  }),
];

function marked(name: string) {
  const a = screen.getByRole("link", { name });
  return {
    a,
    status:
      a.getAttribute("data-link") ?? a.querySelector("[data-link]")?.getAttribute("data-link"),
  };
}

test("same-domain links show whether they are in the library, once resolved", async () => {
  const targets = new Map([
    ["https://d.example/a", "doc-a"],
    ["https://d.example/b#usage", null],
  ]);
  const { rerender } = render(<Reader content={{ doc_version: 1, blocks }} />);
  await screen.findByText("in the library");
  expect(marked("in the library").status).toBeUndefined();

  rerender(<Reader content={{ doc_version: 1, blocks }} linkTargets={targets} />);

  expect(marked("in the library").status).toBe("internal");
  expect(marked("not yet").status).toBe("import");
  expect(marked("in a tab").status).toBe("import");
  expect(marked("elsewhere").status).toBeUndefined();
});

test("same-domain links describe what following them does; external ones open a new tab", async () => {
  render(<Reader content={{ doc_version: 1, blocks }} />);
  await screen.findByText("in the library");

  for (const name of ["in the library", "not yet", "in a tab"]) {
    const { a } = marked(name);
    expect(a.getAttribute("aria-describedby")).toBe(followHelpId);
    expect(a.getAttribute("target")).toBeNull();
  }
  expect(document.getElementById(followHelpId)?.textContent).toContain("importing it first");
  const external = marked("elsewhere").a;
  expect([external.getAttribute("target"), external.getAttribute("rel")]).toEqual([
    "_blank",
    "noopener noreferrer",
  ]);
});

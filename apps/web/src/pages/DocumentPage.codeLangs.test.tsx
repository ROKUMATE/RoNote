import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import type { DocContent } from "../api/schema";
import { json, stubApi } from "../test/api";
import { bringIntoView } from "../test/intersection";
import { renderApp } from "../test/render";

const highlight = vi.hoisted(() =>
  vi.fn(async (code: string, _language: string) => [
    [{ content: code.replace(/\n$/, ""), color: "var(--code-token-keyword)", offset: 0 }],
    [],
  ]),
);
vi.mock("../document/highlighter", () => ({ highlight }));
// Which grammar loads is the languages module's concern; here only the
// language each block is highlighted in matters.
vi.mock("../document/languages", async (actual) => ({
  ...(await actual<typeof import("../document/languages")>()),
  loadLanguage: async () => ({}),
}));

const source = { origin: "source", edited: false } as const;

const content: DocContent = {
  doc_version: 1,
  blocks: [
    { id: "c1", kind: "code", lang: "sh", text: "make build\n", ...source, source_block_id: "c1" },
    {
      id: "l1",
      kind: "list",
      items: [{ children: [{ id: "c2", kind: "code", text: "x := 1\n" }] }],
      ...source,
      source_block_id: "l1",
    },
    { id: "c3", kind: "code", text: "SELECT 1;\n", ...source, source_block_id: "c3" },
  ],
};

let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  highlight.mockClear();
  writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  Element.prototype.scrollIntoView = vi.fn();
});

async function openDoc(stored: DocContent = content) {
  const saves: DocContent[] = [];
  stubApi({
    "GET /documents": () => json({ documents: [] }),
    "GET /documents/doc-1": () =>
      json({
        id: "doc-1",
        title: "Guide",
        source_url: "https://docs.example.com/guide",
        version: 3,
        created_at: "2026-11-04T09:00:00Z",
        updated_at: "2026-11-04T09:00:00Z",
        warnings: [],
        content: stored,
      }),
    "PATCH /documents/doc-1": async (request) => {
      saves.push((await request.clone().json()).content);
      return json({ version: 4 + saves.length });
    },
  });
  const app = renderApp("/");
  await app.router.navigate({ to: "/doc/$id", params: { id: "doc-1" } });
  const main = within(await screen.findByRole("main"));
  await waitFor(() => expect(main.getAllByRole("button", { name: /^Language:/ })).toHaveLength(3));
  const pickers = main.getAllByRole("button", { name: /^Language:/ });
  return { main, saves, pickers };
}

function activeOption(listbox: HTMLElement) {
  return document.getElementById(listbox.getAttribute("aria-activedescendant") ?? "")?.textContent;
}

test("the language is a listbox button: arrows, a letter and Enter choose; the choice is the user's", async () => {
  const { main, saves, pickers } = await openDoc();
  const [, , sql] = pickers;
  if (!sql) throw new Error("three code blocks expected");
  expect(sql.textContent).toBe("Language: plain text");
  expect(sql.getAttribute("aria-haspopup")).toBe("listbox");

  fireEvent.keyDown(sql, { key: "ArrowDown" });

  const listbox = main.getByRole("listbox", { name: "Code language" });
  expect(document.activeElement).toBe(listbox);
  expect(sql.getAttribute("aria-expanded")).toBe("true");
  const options = within(listbox).getAllByRole("option");
  expect(options).toHaveLength(31);
  expect(options[0]?.textContent).toBe("plain text (page)");
  expect(options[0]?.getAttribute("aria-selected")).toBe("true");
  expect(activeOption(listbox)).toBe("plain text (page)");
  fireEvent.keyDown(listbox, { key: "ArrowDown" });
  expect(activeOption(listbox)).toBe("Go");
  fireEvent.keyDown(listbox, { key: "End" });
  expect(activeOption(listbox)).toBe("Plain text");
  fireEvent.keyDown(listbox, { key: "Home" });
  fireEvent.keyDown(listbox, { key: "s" });
  expect(activeOption(listbox)).toBe("Shell session");
  fireEvent.keyDown(listbox, { key: "s" });
  expect(activeOption(listbox)).toBe("Swift");
  fireEvent.keyDown(listbox, { key: "s" });
  expect(activeOption(listbox)).toBe("SQL");
  fireEvent.keyDown(listbox, { key: "Enter" });

  expect(main.queryByRole("listbox")).toBeNull();
  expect(document.activeElement).toBe(sql);
  expect(sql.textContent).toBe("Language: SQL · yours");
  const block = sql.closest(".code-block");
  if (!(block instanceof HTMLElement)) throw new Error("no code block");
  await act(async () => {
    bringIntoView(block);
  });
  await waitFor(() => expect(highlight).toHaveBeenCalledWith("SELECT 1;\n", "sql", {}));
  await waitFor(() => expect(saves).toHaveLength(1), { timeout: 3000 });
  expect(saves[0]?.code_langs).toEqual({ c3: "sql" });

  await waitFor(() => expect(block.querySelector("pre span[style]")).not.toBeNull());
  fireEvent.click(within(block).getByRole("button", { name: "Copy code" }));
  expect(writeText).toHaveBeenCalledWith("SELECT 1;\n");

  fireEvent.click(sql);
  fireEvent.click(within(main.getByRole("listbox")).getByRole("option", { name: "Plain text" }));
  expect(sql.textContent).toBe("Language: Plain text · yours");
  expect(block.querySelector("pre span[style]")).toBeNull();
  expect(block.querySelector("pre")?.textContent).toBe("SELECT 1;\n");
});

test("Escape closes the list without choosing", async () => {
  const { main, saves, pickers } = await openDoc();
  const [shell] = pickers;
  if (!shell) throw new Error("no picker");

  fireEvent.click(shell);
  const listbox = main.getByRole("listbox");
  fireEvent.keyDown(listbox, { key: "ArrowDown" });
  fireEvent.keyDown(listbox, { key: "Escape" });

  expect(main.queryByRole("listbox")).toBeNull();
  expect(document.activeElement).toBe(shell);
  expect(shell.textContent).toBe("Language: sh");
  await new Promise((done) => setTimeout(done, 1200));
  expect(saves).toEqual([]);
});

test("a saved choice opens as the user's, and the page's language takes it back", async () => {
  const { main, saves, pickers } = await openDoc({ ...content, code_langs: { c2: "go" } });
  const [, inList] = pickers;
  if (!inList) throw new Error("no picker");
  expect(inList.textContent).toBe("Language: Go · yours");

  fireEvent.click(inList);
  fireEvent.click(
    within(main.getByRole("listbox")).getByRole("option", { name: "plain text (page)" }),
  );

  expect(inList.textContent).toBe("Language: plain text");
  await waitFor(() => expect(saves).toHaveLength(1), { timeout: 3000 });
  expect(saves[0] && "code_langs" in saves[0]).toBe(false);
});

test("Use for all sets the language of every unlabelled block, and only those", async () => {
  const { main, saves, pickers } = await openDoc();
  const [shell, inList, sql] = pickers;
  if (!shell || !inList || !sql) throw new Error("three pickers expected");
  fireEvent.click(sql);
  fireEvent.click(within(main.getByRole("listbox")).getByRole("option", { name: "SQL" }));

  fireEvent.click(shell);
  fireEvent.click(
    main.getByRole("checkbox", { name: "Use for all unlabelled code in this document" }),
  );
  fireEvent.click(within(main.getByRole("listbox")).getByRole("option", { name: "Go" }));

  expect(pickers.map((p) => p.textContent)).toEqual([
    "Language: Go · yours",
    "Language: Go · yours",
    "Language: SQL · yours",
  ]);
  await waitFor(() => expect(saves.at(-1)?.code_langs).toEqual({ c1: "go", c2: "go", c3: "sql" }), {
    timeout: 3000,
  });
});

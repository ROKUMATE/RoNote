import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import type { IrBlock } from "../../api/schema";
import type { RunnerEvent, RunnerRequest } from "../../runner/protocol";
import { Block } from "../render";
import { openRunner } from "./client";
import { RunScopeProvider } from "./RunArea";

vi.mock("./client", () => ({ openRunner: vi.fn() }));

const blocks: IrBlock[] = [
  { id: "py", kind: "code", lang: "python", text: "print(1)" },
  { id: "net", kind: "code", lang: "python", text: "import requests" },
  { id: "go", kind: "code", lang: "go", text: "fmt.Println(1)" },
  { id: "repl", kind: "code", lang: "pycon", text: ">>> 1 + 1\n2" },
  { id: "js", kind: "code", lang: "js", text: "console.log(1)" },
  { id: "dom", kind: "code", lang: "js", text: 'document.querySelector("p")' },
];

let sent: RunnerRequest[];
let hear: (event: RunnerEvent) => void;
const close = vi.fn();

beforeEach(() => {
  sent = [];
  hear = () => {};
  close.mockClear();
  vi.mocked(openRunner).mockResolvedValue({
    isolated: true,
    send: (request) => sent.push(request),
    listen: (listener) => {
      hear = listener;
      return () => {};
    },
    close,
  });
});

function open() {
  return render(
    <RunScopeProvider documentId="doc" blocks={blocks}>
      {blocks.map((b) => (
        <Block key={b.id} block={b} />
      ))}
    </RunScopeProvider>,
  );
}

function code(id: string) {
  const found = document.querySelector<HTMLElement>(`[data-block-id="${id}"]`);
  if (!found) throw new Error(`no block ${id}`);
  return within(found);
}

/** Runs the py block and waits for the request to reach the runner. */
async function runPython() {
  fireEvent.click(await code("py").findByRole("button", { name: "Run" }));
  await waitFor(() => expect(sent).toHaveLength(1));
}

test("each block says whether it runs, and why on hover and focus", async () => {
  open();
  const py = code("py");
  await py.findByRole("button", {
    name: "Python · browser",
    description: "Python that runs here, in your browser.",
  });
  py.getByRole("button", { name: "Run" });

  const net = code("net");
  await net.findByRole("button", {
    name: "Needs network",
    description: "It reaches the network, which code here can't.",
  });
  expect(net.queryByRole("button", { name: /^Run/ })).toBeNull();
  await code("go").findByRole("button", { name: "Can't run" });
});

test("Escape hides a reason until the chip loses focus", async () => {
  open();
  const chip = await code("net").findByRole("button", { name: "Needs network" });
  const wrap = chip.parentElement;
  fireEvent.keyDown(chip, { key: "Escape" });
  expect(wrap?.dataset.dismissed).toBe("true");
  fireEvent.blur(chip);
  expect(wrap?.dataset.dismissed).toBeUndefined();
});

test("a run shows Python starting, its output labelled, and how it ended", async () => {
  open();
  await runPython();
  const py = code("py");
  expect(sent).toEqual([
    { v: 1, type: "run", id: "py:1", runtime: "python", code: "print(1)", mode: "exec" },
  ]);
  expect(py.getByRole("status").textContent).toBe("Starting Python…");

  act(() => hear({ v: 1, type: "ready", runtime: "python" }));
  expect(py.getByRole("status").textContent).toBe("Running…");
  fireEvent.click(py.getByRole("button", { name: "Stop" }));
  await waitFor(() => expect(sent.at(-1)).toEqual({ v: 1, type: "stop", runtime: "python" }));

  act(() => {
    hear({ v: 1, type: "stdout", id: "py:1", text: "1\n" });
    hear({ v: 1, type: "stdout", id: "py:1", text: "2\n" });
    hear({ v: 1, type: "stderr", id: "py:1", text: "careful\n" });
    hear({ v: 1, type: "result", id: "py:1", text: "None" });
    hear({ v: 1, type: "done", id: "py:1", ms: 12, end: "ok" });
  });
  expect(py.getByRole("status").textContent).toBe("Ran in 12 ms");
  const output = document.querySelector(".run-text");
  expect([...(output?.children ?? [])].map((c) => [c.className, c.textContent])).toEqual([
    ["", "1\n2\n"],
    ["run-stderr", "stderrcareful\n"],
    ["run-result", "valueNone"],
  ]);
  py.getByRole("button", { name: "Run" });
});

test("Restart says the state is gone, and Clear removes the output", async () => {
  open();
  await runPython();
  const py = code("py");
  act(() => hear({ v: 1, type: "done", id: "py:1", ms: 1500, end: "timeout" }));
  expect(py.getByRole("status").textContent).toBe("Stopped at the 30 second limit");

  fireEvent.click(py.getByRole("button", { name: "Restart" }));
  await waitFor(() => expect(sent.at(-1)).toEqual({ v: 1, type: "restart", runtime: "python" }));
  act(() => hear({ v: 1, type: "ended", runtime: "python", reason: "restart" }));
  py.getByText("Python restarted. Earlier results are gone.");

  fireEvent.click(py.getByRole("button", { name: "Clear" }));
  expect(py.queryByRole("status")).toBeNull();
});

test("a transcript runs as one, and a crash says what was lost", async () => {
  open();
  fireEvent.click(await code("repl").findByRole("button", { name: "Run" }));
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0]).toMatchObject({ id: "repl:1", mode: "doctest" });

  act(() => {
    hear({ v: 1, type: "done", id: "repl:1", ms: 3, end: "lost" });
    hear({ v: 1, type: "ended", runtime: "python", reason: "crash" });
  });
  const repl = code("repl");
  expect(repl.getByRole("status").textContent).toBe("Didn't finish");
  repl.getByText(/^Python stopped working, perhaps out of memory/);
});

test("Check examples gives each example's verdict, and a failure's diff", async () => {
  open();
  expect(code("py").queryByRole("button", { name: "Check examples" })).toBeNull();
  fireEvent.click(await code("repl").findByRole("button", { name: "Check examples" }));
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0]).toEqual({
    v: 1,
    type: "run",
    id: "repl:1",
    runtime: "python",
    code: ">>> 1 + 1\n2",
    mode: "check",
  });

  const example = (source: string, want: string, got: string, outcome: "passed" | "failed") =>
    hear({ v: 1, type: "example", id: "repl:1", source, want, got, outcome });
  act(() => {
    hear({ v: 1, type: "ready", runtime: "python" });
    example("1 + 1\n", "2\n", "2\n", "passed");
    example("len('abc')\n", "4\n", "3\n", "failed");
    example("object()\n", "<object object at 0x7f1>\n", "<object object at 0x1a2>\n", "failed");
    hear({ v: 1, type: "done", id: "repl:1", ms: 9, end: "ok" });
  });
  const repl = code("repl");
  expect(repl.getByRole("status").textContent).toBe(
    "1 of 3 examples passed, 1 failed, 1 with a known difference, in 9 ms",
  );
  const items = within(repl.getByRole("list", { name: "Examples" })).getAllByRole("listitem");
  expect(items.map((item) => item.querySelector(".run-verdict")?.textContent)).toEqual([
    "Passed",
    "Failed",
    "Known difference",
  ]);
  expect(items[0]?.textContent).toBe("Passed>>> 1 + 1");
  expect(items[1]?.querySelector(".run-diff")?.textContent).toBe("- 4\n+ 3\n");
  expect(items[2]?.textContent).toContain("addresses change on every run");
});

test("JavaScript runs in its own session, its warnings labelled", async () => {
  open();
  const js = code("js");
  await js.findByRole("button", {
    name: "JavaScript · browser",
    description: "JavaScript that runs here, in your browser.",
  });
  fireEvent.click(js.getByRole("button", { name: "Run" }));
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0]).toEqual({
    v: 1,
    type: "run",
    id: "js:1",
    runtime: "javascript",
    code: "console.log(1)",
    mode: "exec",
  });
  expect(js.getByRole("status").textContent).toBe("Starting JavaScript…");

  act(() => {
    hear({ v: 1, type: "ready", runtime: "javascript" });
    hear({ v: 1, type: "stdout", id: "js:1", text: "1\n" });
    hear({ v: 1, type: "warn", id: "js:1", text: "careful\n" });
    hear({ v: 1, type: "done", id: "js:1", ms: 2, end: "ok" });
  });
  const output = document.querySelector(".run-text");
  expect([...(output?.children ?? [])].map((c) => [c.className, c.textContent])).toEqual([
    ["", "1\n"],
    ["run-warn", "warningcareful\n"],
  ]);

  // Python's news is Python's: a JavaScript Restart says so to the JavaScript block.
  fireEvent.click(js.getByRole("button", { name: "Restart" }));
  await waitFor(() =>
    expect(sent.at(-1)).toEqual({ v: 1, type: "restart", runtime: "javascript" }),
  );
  act(() => hear({ v: 1, type: "ended", runtime: "javascript", reason: "restart" }));
  js.getByText("JavaScript restarted. Earlier results are gone.");

  const dom = code("dom");
  await dom.findByRole("button", {
    name: "Needs a display",
    description: "It needs a window or a display.",
  });
  expect(dom.queryByRole("button", { name: /^Run/ })).toBeNull();
});

test("a runner that never answers is said so, and leaving closes it", async () => {
  vi.mocked(openRunner).mockRejectedValueOnce(new Error("The code runner didn't answer."));
  const page = open();
  fireEvent.click(await code("py").findByRole("button", { name: "Run" }));
  await code("py").findByText("Python couldn't start: the code runner didn't answer.");

  fireEvent.click(code("py").getByRole("button", { name: "Run" }));
  await waitFor(() => expect(sent).toHaveLength(1));
  page.unmount();
  await waitFor(() => expect(close).toHaveBeenCalled());
});

test("blocks outside a document have no controls", async () => {
  render(<Block block={blocks[0] as IrBlock} />);
  await screen.findByRole("button", { name: "Copy code" });
  expect(screen.queryByRole("button", { name: "Run" })).toBeNull();
});

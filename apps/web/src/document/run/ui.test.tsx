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
  expect(sent).toEqual([{ v: 1, type: "run", id: "py:1", code: "print(1)", mode: "exec" }]);
  expect(py.getByRole("status").textContent).toBe("Starting Python…");

  act(() => hear({ v: 1, type: "ready" }));
  expect(py.getByRole("status").textContent).toBe("Running…");
  fireEvent.click(py.getByRole("button", { name: "Stop" }));
  await waitFor(() => expect(sent.at(-1)).toEqual({ v: 1, type: "stop" }));

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
  await waitFor(() => expect(sent.at(-1)).toEqual({ v: 1, type: "restart" }));
  act(() => hear({ v: 1, type: "ended", reason: "restart" }));
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
    hear({ v: 1, type: "ended", reason: "crash" });
  });
  const repl = code("repl");
  expect(repl.getByRole("status").textContent).toBe("Didn't finish");
  repl.getByText(/^Python stopped working, perhaps out of memory/);
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

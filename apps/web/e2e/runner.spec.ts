import type { Frame, Page } from "@playwright/test";
import { stack } from "./stack";
import { expect, test } from "./test";

type Event = {
  type: string;
  id?: string;
  text?: string;
  end?: string;
  reason?: string;
  source?: string;
  want?: string;
  got?: string;
  outcome?: string;
};

declare global {
  interface Window {
    /** The runner the test opened, through the app's client. */
    runner: {
      send(request: object): void;
      listen(listener: (event: Event) => void): () => void;
    };
    /** The events the runner sent the app, in order. */
    runnerEvents: Event[];
  }
}

/**
 * Opens the library with a runner in it, through the app's own client, and
 * returns the runner's frame and whether it is isolated.
 */
async function openRunner(page: Page): Promise<{ frame: Frame; isolated: boolean }> {
  await page.goto("/");
  // A path, not a module specifier: Vite's dev server serves the source.
  const client = "/src/document/run/client.ts";
  const isolated = await page.evaluate(async (client) => {
    const { openRunner } = await import(client);
    const runner = await openRunner();
    window.runner = runner;
    window.runnerEvents = [];
    runner.listen((event: Event) => window.runnerEvents.push(event));
    return runner.isolated as boolean;
  }, client);
  const frame = page.frames().find((f) => f.url().startsWith(stack.runner));
  if (!frame) throw new Error("no runner frame");
  return { frame, isolated };
}

test("the app and its runner are cross-origin isolated", async ({ page }) => {
  const { isolated } = await openRunner(page);
  expect(isolated).toBe(true);
  expect(await page.evaluate(() => crossOriginIsolated)).toBe(true);
});

test("the runner answers the app, and no other origin or window", async ({ page }) => {
  const { frame } = await openRunner(page);
  const hello = (origin: string, source: "parent" | "self") =>
    frame.evaluate(
      ([origin, source]) =>
        window.dispatchEvent(
          new MessageEvent("message", {
            data: { v: 1, type: "hello" },
            origin,
            source: source === "parent" ? window.parent : window,
          }),
        ),
      [origin, source] as const,
    );
  await hello(stack.fixtures, "parent");
  await hello(stack.runner, "parent");
  await hello(stack.web, "self");
  // Answers arrive in order, so once this one is in, any to the others would be.
  await hello(stack.web, "parent");

  await expect.poll(() => page.evaluate(() => window.runnerEvents.length)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.runnerEvents)).toEqual([
    { v: 1, type: "hello", isolated: true },
  ]);
});

// COEP would refuse the answer anyway, as the API sends no CORP header; the
// request itself would still go, so these look for CSP's refusal to send it.
test("code in the runner can't reach the API, directly or through the app", async ({ page }) => {
  const { frame } = await openRunner(page);
  const reach = (url: string) =>
    frame.evaluate(async (url) => {
      const refused: string[] = [];
      const listener = (event: SecurityPolicyViolationEvent) =>
        refused.push(event.effectiveDirective);
      document.addEventListener("securitypolicyviolation", listener);
      const fetched = await fetch(url, { mode: "no-cors" }).then(
        () => "reached",
        () => "failed",
      );
      await new Promise((resolve) => setTimeout(resolve, 100));
      document.removeEventListener("securitypolicyviolation", listener);
      return `${fetched}, refused by ${refused.join() || "nothing"}`;
    }, url);

  expect(await reach(`${stack.api}/readyz`)).toBe("failed, refused by connect-src");
  expect(await reach(`${stack.web}/api/v1/documents`)).toBe("failed, refused by connect-src");
  expect(await reach(`${stack.runner}/`), "its own origin").toBe("reached, refused by nothing");
});

test("the runner can't read the app's cookies or storage", async ({ page }) => {
  const { frame } = await openRunner(page);
  await page.context().addCookies([{ name: "ronote_secret", value: "1", url: stack.web }]);
  await page.evaluate(() => {
    localStorage.setItem("ronote_secret", "1");
    sessionStorage.setItem("ronote_secret", "1");
  });

  const seen = await frame.evaluate(() => {
    const read = (value: () => unknown) => {
      try {
        return String(value());
      } catch {
        return "refused";
      }
    };
    return {
      cookies: document.cookie,
      local: localStorage.getItem("ronote_secret"),
      session: sessionStorage.getItem("ronote_secret"),
      parentCookies: read(() => window.parent.document.cookie),
      parentLocal: read(() => window.parent.localStorage.getItem("ronote_secret")),
    };
  });
  expect(seen).toEqual({
    cookies: "",
    local: null,
    session: null,
    parentCookies: "refused",
    parentLocal: "refused",
  });
});

test("only the app may frame the runner", async ({ page }) => {
  await page.goto(`${stack.fixtures}/robots.txt`);
  await page.evaluate((src) => {
    const frame = document.createElement("iframe");
    frame.src = src;
    document.body.append(frame);
  }, `${stack.runner}/`);

  await expect.poll(() => page.frames().length).toBe(2);
  const frame = page.frames()[1];
  await expect.poll(() => frame?.url()).not.toBe("about:blank");
  expect(frame?.url()).not.toContain(stack.runner);
});

/** Runs code in the page's runner, resolving with the run's events, done last. */
function run(
  page: Page,
  code: string,
  mode: "exec" | "doctest" | "check" = "exec",
  runtime: "python" | "javascript" = "python",
): Promise<Event[]> {
  return page.evaluate(
    ([code, mode, runtime]) =>
      new Promise<Event[]>((resolve) => {
        const id = crypto.randomUUID();
        const events: Event[] = [];
        const stop = window.runner.listen((event) => {
          if (event.id !== id) return;
          events.push(event);
          if (event.type !== "done") return;
          stop();
          resolve(events);
        });
        window.runner.send({ v: 1, type: "run", id, runtime, code, mode });
      }),
    [code, mode, runtime] as const,
  );
}

/**
 * The parts of events worth comparing: no ids or durations, and output of one
 * kind in a row joined, however the runner batched it.
 */
function shown(events: Event[]) {
  const parts: Event[] = [];
  for (const { type, text, end } of events) {
    const last = parts.at(-1);
    if (text && last?.type === type && last.text) last.text += text;
    else parts.push({ type, ...(text && { text }), ...(end && { end }) });
  }
  return parts;
}

test("Python runs blocks in one session, and Restart clears it", async ({ page }) => {
  await openRunner(page);
  expect(shown(await run(page, "x = 41"))).toEqual([{ type: "done", end: "ok" }]);
  expect(shown(await run(page, "print(x + 1)\nx * 2"))).toEqual([
    { type: "stdout", text: "42\n" },
    { type: "result", text: "82" },
    { type: "done", end: "ok" },
  ]);
  expect(await page.evaluate(() => window.runnerEvents.map((e) => e.type).slice(0, 2))).toEqual([
    "starting",
    "ready",
  ]);

  await page.evaluate(() => window.runner.send({ v: 1, type: "restart", runtime: "python" }));
  const after = await run(page, "print(x)");
  expect(after.at(-1)?.end).toBe("error");
  expect(after[0]?.text).toContain("NameError: name 'x' is not defined");
  expect(await page.evaluate(() => window.runnerEvents.filter((e) => e.type === "ended"))).toEqual([
    { v: 1, type: "ended", runtime: "python", reason: "restart" },
  ]);
});

test("a traceback starts at the reader's code, and a transcript runs like the REPL", async ({
  page,
}) => {
  await openRunner(page);
  const [error] = await run(page, "def f():\n    return 1 / 0\n\nf()");
  expect(error?.type).toBe("error");
  expect(error?.text).toMatch(
    /^Traceback \(most recent call last\):\n {2}File "<cell \d+>", line 4/,
  );
  expect(error?.text).toContain("    return 1 / 0");
  expect(error?.text).toMatch(/ZeroDivisionError: division by zero\n$/);
  expect(error?.text).not.toContain("_pyodide");

  const transcript = ">>> 6 * 7\n42\n>>> print('hi')\nhi\n>>> for i in range(2):\n...     i\n0\n1";
  expect(shown(await run(page, transcript, "doctest"))).toEqual([
    { type: "stdout", text: "42\nhi\n" },
    { type: "done", end: "ok" },
  ]);
});

test("a check compares each example with the page's output, in the session", async ({ page }) => {
  await openRunner(page);
  const transcript = [
    ">>> 6 * 7",
    "42",
    ">>> print('hi')",
    "bye",
    ">>> 1 / 0",
    "Traceback (most recent call last):",
    "  ...",
    "ZeroDivisionError: division by zero",
    ">>> int('x')",
    "Traceback (most recent call last):",
    "  ...",
    "ValueError: other words",
    ">>> nowhere_at_all",
    ">>> kept = 5",
  ].join("\n");
  const events = await run(page, transcript, "check");
  expect(
    events.map(({ type, source, want, got, outcome, end }) =>
      type === "example" ? { source, want, got, outcome } : { type, end },
    ),
  ).toEqual([
    { source: "6 * 7\n", want: "42\n", got: "42\n", outcome: "passed" },
    { source: "print('hi')\n", want: "bye\n", got: "hi\n", outcome: "failed" },
    {
      source: "1 / 0\n",
      want: "Traceback (most recent call last):\n  ...\nZeroDivisionError: division by zero\n",
      got: "Traceback (most recent call last):\nZeroDivisionError: division by zero\n",
      outcome: "passed",
    },
    {
      source: "int('x')\n",
      want: "Traceback (most recent call last):\n  ...\nValueError: other words\n",
      got: "Traceback (most recent call last):\nValueError: invalid literal for int() with base 10: 'x'\n",
      outcome: "failed",
    },
    {
      source: "nowhere_at_all\n",
      want: "",
      got: "Traceback (most recent call last):\nNameError: name 'nowhere_at_all' is not defined\n",
      outcome: "error",
    },
    { source: "kept = 5\n", want: "", got: "", outcome: "passed" },
    { type: "done", end: "ok" },
  ]);
  expect(shown(await run(page, "kept"))).toEqual([
    { type: "result", text: "5" },
    { type: "done", end: "ok" },
  ]);
});

test("Stop interrupts an endless loop within a second, and keeps the session", async ({ page }) => {
  await openRunner(page);
  await run(page, "kept = 'still here'");
  const { end, ms } = await page.evaluate(
    () =>
      new Promise<{ end: string | undefined; ms: number }>((resolve) => {
        let stopped = 0;
        const stop = window.runner.listen((event) => {
          if (event.id !== "loop" || event.type !== "done") return;
          stop();
          resolve({ end: event.end, ms: performance.now() - stopped });
        });
        window.runner.send({
          v: 1,
          type: "run",
          id: "loop",
          runtime: "python",
          code: "while True:\n    pass",
          mode: "exec",
        });
        setTimeout(() => {
          stopped = performance.now();
          window.runner.send({ v: 1, type: "stop", runtime: "python" });
        }, 500);
      }),
  );
  expect(end).toBe("stop");
  expect(ms).toBeLessThan(1000);
  expect(shown(await run(page, "kept"))).toEqual([
    { type: "result", text: "'still here'" },
    { type: "done", end: "ok" },
  ]);
});

test("output past 1 MB stops the run, and the page stays responsive", async ({ page }) => {
  await openRunner(page);
  await run(page, "pass");
  const { output, end, slowest } = await page.evaluate(
    () =>
      new Promise<{ output: number; end: string | undefined; slowest: number }>((resolve) => {
        let output = 0;
        let slowest = 0;
        let last = performance.now();
        const tick = setInterval(() => {
          slowest = Math.max(slowest, performance.now() - last);
          last = performance.now();
        }, 0);
        const stop = window.runner.listen((event) => {
          if (event.id !== "flood") return;
          if (event.type === "stdout") output += event.text?.length ?? 0;
          if (event.type !== "done") return;
          stop();
          clearInterval(tick);
          resolve({ output, end: event.end, slowest });
        });
        const code = "while True:\n    print('x' * 999)";
        window.runner.send({
          v: 1,
          type: "run",
          id: "flood",
          runtime: "python",
          code,
          mode: "exec",
        });
      }),
  );
  expect(end).toBe("output");
  expect(output).toBe(1_000_000);
  expect(slowest).toBeLessThan(250);
});

test("Python can't reach the API or the page", async ({ page }) => {
  await openRunner(page);
  const fetched = await run(
    page,
    [
      "import asyncio, js",
      "from pyodide.ffi import create_proxy",
      "refused = []",
      'js.addEventListener("securitypolicyviolation", create_proxy(lambda e: refused.append(e.effectiveDirective)))',
      "try:",
      `    await js.fetch("${stack.api}/readyz", js.Object.fromEntries([["mode", "no-cors"]]))`,
      "except Exception as error:",
      "    print(type(error).__name__)",
      "await asyncio.sleep(0.1)",
      "refused",
    ].join("\n"),
  );
  expect(shown(fetched)).toEqual([
    { type: "stdout", text: "JsException\n" },
    { type: "result", text: "['connect-src']" },
    { type: "done", end: "ok" },
  ]);
  const [noPage] = await run(page, "import js\njs.document");
  expect(noPage?.text).toContain("AttributeError");
  const [noPackages] = await run(page, "import numpy");
  expect(noPackages?.text).toMatch(/ModuleNotFoundError: No module named 'numpy'\n$/);
});

const js = (page: Page, code: string) => run(page, code, "exec", "javascript");

test("JavaScript blocks share one session, may await, and give a value", async ({ page }) => {
  await openRunner(page);
  expect(
    shown(
      await js(
        page,
        "const base = await Promise.resolve(40);\nconsole.log('base', base, { ok: true });",
      ),
    ),
  ).toEqual([
    { type: "stdout", text: "base 40 {ok: true}\n" },
    { type: "done", end: "ok" },
  ]);
  // Declared again, as a block run twice would be; name is the worker's own, too.
  // biome-ignore lint/suspicious/noTemplateCurlyInString: the reader's JavaScript, as text.
  const again = "const name = 'Ada';\nconst base = 1;\n`${name} ${base + 1}`";
  expect(shown(await js(page, again))).toEqual([
    { type: "result", text: "'Ada 2'" },
    { type: "done", end: "ok" },
  ]);
  // A run lasts until the timers it set have fired.
  expect(
    shown(
      await js(
        page,
        "for (let step = 0; step < 3; step++) {\n  setTimeout(() => console.log(step), 10);\n}",
      ),
    ),
  ).toEqual([
    { type: "stdout", text: "0\n1\n2\n" },
    { type: "done", end: "ok" },
  ]);
  expect(shown(await js(page, "console.warn('careful');\nconsole.error('broken');"))).toEqual([
    { type: "warn", text: "careful\n" },
    { type: "stderr", text: "broken\n" },
    { type: "done", end: "ok" },
  ]);
});

test("an uncaught JavaScript error starts at the reader's code, and a rejection says so", async ({
  page,
}) => {
  await openRunner(page);
  const thrown = await js(page, "function f() {\n  return missing + 1;\n}\nf();");
  expect(shown(thrown).map((e) => e.type)).toEqual(["error", "done"]);
  expect(thrown[0]?.text).toMatch(
    /^Uncaught ReferenceError: missing is not defined\n {4}at f \(cell \d+:2:3\)\n {4}at cell \d+:4:1\n$/,
  );
  expect(thrown.at(-1)?.end).toBe("error");

  const rejected = await js(page, 'Promise.reject(new Error("nobody caught this"));');
  expect(shown(rejected).map((e) => e.type)).toEqual(["result", "error", "done"]);
  expect(rejected[0]?.text).toBe("Promise {…}");
  expect(rejected[1]?.text).toMatch(
    /^Uncaught \(in promise\) Error: nobody caught this\n {4}at cell \d+:1:16\n$/,
  );

  const [syntax] = await js(page, "const = 1");
  expect(syntax?.text).toBe("Uncaught SyntaxError: Unexpected token (1:6)\n");
});

test("Stop ends a JavaScript loop at once, and the session with it", async ({ page }) => {
  await openRunner(page);
  await js(page, "const kept = 'gone after Stop';");
  const { end, ms } = await page.evaluate(
    () =>
      new Promise<{ end: string | undefined; ms: number }>((resolve) => {
        let stopped = 0;
        const stop = window.runner.listen((event) => {
          if (event.id !== "spin" || event.type !== "done") return;
          stop();
          resolve({ end: event.end, ms: performance.now() - stopped });
        });
        window.runner.send({
          v: 1,
          type: "run",
          id: "spin",
          runtime: "javascript",
          code: "while (true) {}",
          mode: "exec",
        });
        setTimeout(() => {
          stopped = performance.now();
          window.runner.send({ v: 1, type: "stop", runtime: "javascript" });
        }, 300);
      }),
  );
  expect(end).toBe("stop");
  expect(ms).toBeLessThan(1000);
  expect(shown(await js(page, "typeof kept"))).toEqual([
    { type: "result", text: "'undefined'" },
    { type: "done", end: "ok" },
  ]);
});

test("JavaScript can't reach the API or the page", async ({ page }) => {
  await openRunner(page);
  const reached = await js(
    page,
    [
      "const refused = [];",
      "self.addEventListener('securitypolicyviolation', (e) => refused.push(e.effectiveDirective));",
      "let outcome;",
      "try {",
      `  await fetch("${stack.api}/readyz", { mode: "no-cors" });`,
      "  outcome = 'reached';",
      "} catch {",
      "  outcome = 'failed';",
      "}",
      "await new Promise((resolve) => setTimeout(resolve, 100));",
      // biome-ignore lint/suspicious/noTemplateCurlyInString: the reader's JavaScript, as text.
      "`${outcome}, refused by ${refused.join() || 'nothing'}, document is ${typeof document}`",
    ].join("\n"),
  );
  expect(shown(reached)).toEqual([
    { type: "result", text: "'failed, refused by connect-src, document is undefined'" },
    { type: "done", end: "ok" },
  ]);
});

import { expect, test } from "vitest";
import { diff, type Example, known, prompt, summary } from "./doctest";
import { pythonVersion } from "./python";

const failed = (want: string, got: string): Example => ({
  source: "x\n",
  want,
  got,
  outcome: "failed",
});

const traceback = (last: string) => `Traceback (most recent call last):\n  ...\n${last}\n`;

test("an object whose address alone differs is a known difference", () => {
  expect(
    known(
      failed(
        "<__main__.Thing object at 0x7f3a2c1e5d90>\n",
        "<__main__.Thing object at 0x1a2b3c>\n",
      ),
    ),
  ).toBe("address");
  expect(known(failed("<Thing at 0x1f>\n", "<Other at 0x2e>\n"))).toBeUndefined();
  // docs.python.org's tutorial shows one without its 0x.
  expect(known(failed("<function fib at 10042ed0>\n", "<function fib at 0x195c968>\n"))).toBe(
    "address",
  );
  expect(known(failed("at 10042ed0\n", "at 195c968\n"))).toBeUndefined();
  expect(known(failed("'0xff'\n", "'0xfe'\n"))).toBeUndefined();
});

test("the same exception in other words is a known difference between versions", () => {
  expect(
    known(
      failed(
        traceback("NameError: name 'spam' is not defined"),
        "Traceback (most recent call last):\nNameError: name 'spam' is not defined. Did you mean: 'span'?\n",
      ),
    ),
  ).toBe("version");
  expect(
    known({
      ...failed(
        traceback("decimal.InvalidOperation: [<class 'decimal.ConversionSyntax'>]"),
        traceback("InvalidOperation: [<class 'decimal.ConversionSyntax'>]"),
      ),
      outcome: "error",
    }),
  ).toBe("version");
});

test("other differences are the example's, not known", () => {
  expect(known(failed("4\n", "3\n"))).toBeUndefined();
  expect(
    known(failed(traceback("KeyError: 'a'"), traceback("IndexError: list index out of range"))),
  ).toBeUndefined();
  expect(known(failed("", traceback("ZeroDivisionError: division by zero")))).toBeUndefined();
  expect(known({ ...failed("1\n", "1\n"), outcome: "passed" })).toBeUndefined();
});

test("a diff keeps the common lines and marks the rest, expected first", () => {
  expect(diff("4\n", "3\n")).toEqual([
    { kind: "want", text: "4" },
    { kind: "got", text: "3" },
  ]);
  expect(diff("cat 3\nwindow 6\ndefenestrate 12\n", "cat 3\nwindow 7\ndefenestrate 12\n")).toEqual([
    { kind: "same", text: "cat 3" },
    { kind: "want", text: "window 6" },
    { kind: "got", text: "window 7" },
    { kind: "same", text: "defenestrate 12" },
  ]);
  expect(diff("", "hi\n")).toEqual([{ kind: "got", text: "hi" }]);
  expect(diff("a\n\nb\n", "a\n\nb\n").map((l) => l.kind)).toEqual(["same", "same", "same"]);
});

test("an example's source reads as the page wrote it", () => {
  expect(prompt("6 * 7\n")).toBe(">>> 6 * 7");
  expect(prompt("for w in words:\n    print(w)\n")).toBe(">>> for w in words:\n...     print(w)");
});

test("the summary counts passes, failures and known differences apart", () => {
  const passed: Example = { source: "1\n", want: "1\n", got: "1\n", outcome: "passed" };
  const address = failed("<A at 0x1>\n", "<A at 0x2>\n");
  expect(summary([passed])).toBe("The example passed");
  expect(summary([passed, passed])).toBe("All 2 examples passed");
  expect(summary([passed, failed("4\n", "3\n")])).toBe("1 of 2 examples passed, 1 failed");
  expect(summary([passed, address, failed("4\n", "3\n")])).toBe(
    "1 of 3 examples passed, 1 failed, 1 with a known difference",
  );
  expect(summary([])).toBe("Found no examples to check");
});

const packages = import.meta.glob<{ dependencies: Record<string, string> }>(
  "../../../package.json",
  {
    eager: true,
    import: "default",
  },
);

test("the Python version the labels name is the one Pyodide's version says", () => {
  const pyodide = Object.values(packages)[0]?.dependencies.pyodide ?? "";
  // Pyodide 314.x runs Python 3.14.
  expect(pyodide.split(".")[0]).toBe(pythonVersion.replace(".", ""));
});

import { expect, test } from "vitest";
import type { IrBlock } from "../../api/schema";
import {
  type CodeBlock,
  classify,
  classifyAll,
  codeBlocks,
  doctestSource,
  type RunClass,
  reasons,
} from "./classify";

const cases: [string, string | undefined, RunClass][] = [
  ['print("hello")', "python", "browser_python"],
  ["def area(r):\n    return 3.14159 * r * r\n\narea(2)", "py", "browser_python"],
  [
    "import json, math\nfrom collections import Counter\nCounter('abca')",
    "python",
    "browser_python",
  ],
  [">>> 1 + 1\n2\n>>> for i in range(2):\n...     print(i)\n0\n1", "pycon", "python_doctest"],
  [">>> import requests\n>>> requests.get('https://example.org')", "python", "needs_network"],
  ["import numpy as np\nnp.zeros(3)", "python", "needs_packages"],
  ["from langchain_openai import ChatOpenAI", "python", "needs_packages"],
  ["import requests\nrequests.get('https://example.org')", "python", "needs_network"],
  ["from urllib.request import urlopen", "python", "needs_network"],
  ["import urllib.request", "python", "needs_network"],
  ['import os\nkey = os.environ["OPENAI_API_KEY"]', "python", "needs_secret"],
  ['client = Client(api_key="sk-...")', "python", "needs_secret"],
  ['token = os.getenv("GITHUB_TOKEN")', "python", "needs_secret"],
  ['import os\nprint(os.environ["HOME"])', "python", "browser_python"],
  ['name = input("Your name? ")\nprint(name)', "python", "needs_input"],
  ["import sys\nprint(sys.argv[1])", "python", "needs_input"],
  ["import tkinter as tk\ntk.Tk()", "python", "needs_dom"],
  ["from js import document\ndocument.title", "python", "needs_dom"],
  ["pip install notesplit", "bash", "shell"],
  ["$ ls -la", "console", "shell"],
  ["import subprocess\nsubprocess.run(['ls'])", "python", "shell"],
  ["$ python -m venv .venv", "python", "shell"],
  ["    return x + 1", "python", "fragment"],
  ['case 401 | 403 | 404:\n    return "Not allowed"', "python", "fragment"],
  ["elif x > 0:\n    print(x)", "python", "fragment"],
  ["else:\n    pass", "python", "fragment"],
  ["except ValueError:\n    pass", "python", "fragment"],
  ["return result", "python", "fragment"],
  ["case = 1\nprint(case)", "python", "browser_python"],
  ["for item in items:", "python", "fragment"],
  ["total = sum(\n    values,", "python", "fragment"],
  ["open(<path>)", "python", "fragment"],
  ['print("still open)', "python", "fragment"],
  ["fmt.Println(1)", "go", "unsupported"],
  ['print("hello")', undefined, "unsupported"],
  ["a = 1\nprint(a < b)", "python", "browser_python"],
  ['print("<not> a placeholder")', "python", "browser_python"],
  ["# A comment saying input(\nx = 2", "python", "browser_python"],
];

test.each(cases)("%s (%s) is %s", (code, lang, want) => {
  expect(classify(code, lang).class).toBe(want);
});

test("every class has a plain sentence, and only fragments and packages run anyway", () => {
  const runs = Object.fromEntries(
    (Object.keys(reasons) as RunClass[]).map((c) => {
      const sample = cases.find(([, , k]) => k === c);
      if (!sample) throw new Error(`no sample of ${c}`);
      return [c, classify(sample[0], sample[1]).run];
    }),
  );
  expect(runs).toEqual({
    browser_python: "run",
    python_doctest: "run",
    needs_packages: "anyway",
    fragment: "anyway",
    needs_network: "none",
    needs_secret: "none",
    needs_input: "none",
    needs_dom: "none",
    shell: "none",
    unsupported: "none",
  });
  for (const sentence of Object.values(reasons)) expect(sentence).toMatch(/^[A-Z].*\.$/);
});

test("a doctest's source is its prompted lines, without the prompts or the output", () => {
  expect(doctestSource(">>> x = 1\n>>> if x:\n...     print(x)\n1\n")).toBe(
    "x = 1\nif x:\n    print(x)",
  );
});

const doc = (...blocks: [string, string?][]): CodeBlock[] =>
  blocks.map(([code, lang = "python"], i) => ({ id: `b${i}`, code, lang }));

const across: [string, CodeBlock[], RunClass[]][] = [
  [
    "a name from a runnable block keeps it runnable",
    doc(["def area(r):\n    return 3 * r * r"], ["print(area(2))"]),
    ["browser_python", "browser_python"],
  ],
  [
    "a name only a package block defines needs its packages",
    doc(["import numpy as np"], ["print(np.zeros(3))"]),
    ["needs_packages", "needs_packages"],
  ],
  [
    "a name from a block that can't run can't run either, through a chain",
    doc(
      ['from openai import OpenAI\nclient = OpenAI(api_key="sk-...")'],
      ["def ask(q):\n    return client.ask(q)"],
      ['print(ask("hi"))'],
    ),
    ["needs_secret", "needs_secret", "needs_secret"],
  ],
  [
    "the stricter class wins when a name comes from both kinds",
    doc(["import numpy as np"], ['key = os.environ["API_KEY"]'], ["print(np, key)"]),
    ["needs_packages", "needs_secret", "needs_secret"],
  ],
  [
    "a name that a runnable block also defines keeps it runnable",
    doc(["import numpy as np\ndata = np.ones(3)"], ["data = [1, 2]"], ["print(data)"]),
    ["needs_packages", "browser_python", "browser_python"],
  ],
  [
    "a name no block defines makes a fragment",
    doc(["score = 0.7 * vector_similarity + 0.3 * keyword_overlap"]),
    ["fragment"],
  ],
  [
    "a star import may define any name",
    doc(["from math import *"], ["print(floor(pi))"]),
    ["browser_python", "browser_python"],
  ],
  [
    "a star import from a package passes on its packages",
    doc(["from numpy import *"], ["print(zeros(3))"]),
    ["needs_packages", "needs_packages"],
  ],
  [
    "parameters, loop and comprehension variables, keywords and attributes aren't needed",
    doc([
      "def f(a, b=2, *rest, **opts):\n    return [x * a for x in rest] + list(opts)\n" +
        "g = lambda y: y.real\nfor i, j in enumerate('ab'):\n    print(i, j, f(1, b=3), g(2))",
    ]),
    ["browser_python"],
  ],
  [
    "names inside strings, comments and f-string prefixes aren't needed",
    doc(['name = "x"\nprint(f"{name} and missing")  # uses missing\nprint(rb"raw", 1e3, 0x1F)']),
    ["browser_python"],
  ],
  [
    "a doctest reads its own prompted lines",
    doc([">>> import requests"], [">>> r = 1\n>>> r + 1\n2"]),
    ["needs_network", "python_doctest"],
  ],
  [
    "a function's parameters and locals define nothing for other blocks",
    doc(
      ["def where_is(point):\n    found = point\n    return found"],
      ["print(where_is(1))"],
      ["print(point, found)"],
    ),
    ["browser_python", "browser_python", "fragment"],
  ],
  [
    "a class's body and a call's keywords define nothing either",
    doc(
      ["class Point:\n    def __init__(self, x, y):\n        self.x = x"],
      ["Point(x=1, y=2)"],
      ["x"],
    ),
    ["browser_python", "browser_python", "fragment"],
  ],
  [
    "a match on a name nothing defines is a fragment, though its patterns bind",
    doc(
      [
        "match point:\n    case (0, y):\n        print(y)\n    case [first, *rest] if first:\n        print(rest)",
      ],
      [
        "spot = (0, 1)\nmatch spot:\n    case (x, 0) | (0, x):\n        print(x)\n    case Point(x=px) as p:\n        print(px, p)",
      ],
      ["class Point:\n    pass"],
    ),
    ["fragment", "browser_python", "browser_python"],
  ],
  [
    "names bound under a top-level if, for or with, or declared global, count",
    doc(
      ["if True:\n    limit = 3\nfor i in range(2):\n    last = i"],
      ["def setup():\n    global ready\n    ready = True"],
      ["print(limit, last, i, ready)"],
    ),
    ["browser_python", "browser_python", "browser_python"],
  ],
  [
    "a signature over several lines keeps its body local",
    doc(
      ["def area(\n    width,\n    height,\n):\n    size = width * height\n    return size"],
      ["size"],
    ),
    ["browser_python", "fragment"],
  ],
  [
    "shell and other languages pass nothing on",
    doc(["export OPENAI_API_KEY=sk", "bash"], ["const x = 1", "js"], ["print(1)"]),
    ["shell", "unsupported", "browser_python"],
  ],
];

test.each(across)("%s", (_, blocks, want) => {
  const classes = classifyAll(blocks);
  expect(blocks.map((b) => classes.get(b.id)?.class)).toEqual(want);
});

// Every code block of every golden fixture, as the classifier sees it now.
// Review a change to classes.json like a change to an extractor golden.
const goldens = import.meta.glob<{ blocks: IrBlock[] }>(
  "../../../../../testdata/golden/*/expected.json",
  { eager: true, import: "default" },
);

test("every golden fixture's code blocks keep their classes", async () => {
  const classes: Record<string, string[]> = {};
  for (const [path, golden] of Object.entries(goldens)) {
    const blocks = codeBlocks(golden.blocks);
    const found = classifyAll(blocks);
    if (blocks.length > 0) {
      classes[path.split("/").at(-2) ?? path] = blocks.map(
        (b) => `${b.lang ?? "-"}: ${found.get(b.id)?.class}`,
      );
    }
  }
  expect(Object.keys(classes).length).toBeGreaterThan(30);
  await expect(`${JSON.stringify(classes, null, 2)}\n`).toMatchFileSnapshot(
    "../../../../../testdata/runnability/classes.json",
  );
});

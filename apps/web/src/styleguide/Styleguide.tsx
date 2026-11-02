import { useLayoutEffect, useRef, useState } from "react";
import { CodeBlock } from "../ui/CodeBlock";
import { PageTitle } from "../ui/PageTitle";

const colorTokens = [
  "paper",
  "paper-2",
  "ink",
  "ink-2",
  "rule",
  "accent",
  "mark",
  "gen",
  "selection",
] as const;

const typeScale = [
  { name: "text-ui", sample: "UI text, 13px IBM Plex Sans", className: "font-sans text-ui" },
  { name: "text-ui-lg", sample: "UI text, 14px IBM Plex Sans", className: "font-sans text-ui-lg" },
  {
    name: "text-body",
    sample: "Reader body, 18px Source Serif 4",
    className: "font-serif text-body",
  },
  { name: "text-h3", sample: "Heading 3, 19px", className: "font-sans font-semibold text-h3" },
  { name: "text-h2", sample: "Heading 2, 24px", className: "font-sans font-semibold text-h2" },
  { name: "text-h1", sample: "Heading 1, 32px", className: "font-sans font-semibold text-h1" },
  { name: "text-code", sample: "Code, 14px JetBrains Mono", className: "font-mono text-code" },
] as const;

const goSample = `package fetch

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// fetchAll fetches every URL concurrently and returns the bodies in order.
func fetchAll(ctx context.Context, urls []string, fetch func(context.Context, string) (string, error)) ([]string, error) {
	bodies := make([]string, len(urls))
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Go(func() {
			bodies[i], errs[i] = fetch(ctx, url)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", urls[i], err)
		}
	}
	return bodies, nil
}

// timeout bounds a whole batch of requests.
var timeout = 10 * time.Second`;

const pythonSample = `result = sorted((record for record in load_records("library.jsonl") if record.get("language") == "python" and record.get("stars", 0) > 1000), key=lambda record: (record["stars"], record["name"]), reverse=True)`;

/** Dev-only reference for tokens, the type scale and reader typography. */
export function Styleguide() {
  return (
    <div className="max-w-[68ch] space-y-12">
      <div>
        <PageTitle>Styleguide</PageTitle>
        <p className="mt-3 text-ink-2 text-ui-lg">
          Dev builds only. Every token in both themes, the type scale and a sample document.
        </p>
      </div>

      <section aria-labelledby="tokens-heading" className="space-y-4">
        <h2 id="tokens-heading" className="font-semibold text-h2">
          Tokens
        </h2>
        <div className="grid gap-4 sidebar:grid-cols-2">
          <TokenPanel theme="light" />
          <TokenPanel theme="dark" />
        </div>
      </section>

      <section aria-labelledby="type-heading" className="space-y-4">
        <h2 id="type-heading" className="font-semibold text-h2">
          Type scale
        </h2>
        <dl className="space-y-3">
          {typeScale.map(({ name, sample, className }) => (
            <div key={name}>
              <dt className="font-mono text-ink-2 text-ui">{name}</dt>
              <dd className={className}>{sample}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section aria-labelledby="sample-heading" className="space-y-4">
        <h2 id="sample-heading" className="font-semibold text-h2">
          Sample document
        </h2>
        <SampleDocument />
      </section>
    </div>
  );
}

function TokenPanel({ theme }: { theme: "light" | "dark" }) {
  return (
    <div data-theme={theme} className="rounded-md border border-rule bg-paper p-4 text-ink">
      <h3 className="mb-3 font-semibold text-ui-lg capitalize">{theme}</h3>
      <ul className="space-y-2">
        {colorTokens.map((token) => (
          <ColorSwatch key={token} token={token} />
        ))}
      </ul>
      <div className="mt-4 flex items-center gap-3 text-ui">
        <span className="size-8 rounded-sm border border-rule bg-paper-2" />
        <span>radius-sm 3px</span>
        <span className="size-8 rounded-md border border-rule bg-paper-2" />
        <span>radius-md 5px</span>
      </div>
      <div className="mt-4 text-ui">
        <span
          className="inline-block rounded-sm px-2 py-1"
          style={{ outline: "var(--focus-ring)", outlineOffset: "var(--focus-ring-offset)" }}
        >
          focus ring
        </span>
      </div>
    </div>
  );
}

/** A colour chip with the token name and the colour it resolves to here. */
function ColorSwatch({ token }: { token: (typeof colorTokens)[number] }) {
  const chip = useRef<HTMLSpanElement>(null);
  const [resolved, setResolved] = useState("");

  useLayoutEffect(() => {
    if (chip.current) {
      setResolved(getComputedStyle(chip.current).backgroundColor);
    }
  }, []);

  return (
    <li className="flex items-center gap-3 text-ui">
      <span
        ref={chip}
        className="size-8 shrink-0 rounded-sm border border-rule"
        style={{ backgroundColor: `var(--color-${token})` }}
      />
      <span className="min-w-0">
        <code className="block font-mono">{token}</code>
        <span className="block break-all font-mono text-ink-2">{resolved}</span>
      </span>
    </li>
  );
}

function SampleDocument() {
  return (
    <article className="reader">
      <h1>Waiting for many requests at once</h1>
      <p>
        A <code>sync.WaitGroup</code> counts running goroutines. Since Go 1.25 the <code>Go</code>{" "}
        method starts a goroutine and tracks it in one call, so there is no separate{" "}
        <code>Add</code> to forget. <mark>The function below never returns partial results</mark>:
        it reports the first error it finds instead.
      </p>

      <h2>Fetching in parallel</h2>
      <p>Each goroutine writes only its own slot, so the slices need no lock.</p>
      <CodeBlock code={goSample} />

      <h3>A long line</h3>
      <p>Code keeps its line breaks. Long lines scroll sideways instead of wrapping:</p>
      <CodeBlock code={pythonSample} />

      <h2>When to use what</h2>
      <table>
        <thead>
          <tr>
            <th scope="col">Tool</th>
            <th scope="col">Use it when</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <code>sync.WaitGroup</code>
            </td>
            <td>You only need to wait for every goroutine to finish.</td>
          </tr>
          <tr>
            <td>
              <code>errgroup.Group</code>
            </td>
            <td>The first error should cancel the rest.</td>
          </tr>
          <tr>
            <td>Channels</td>
            <td>Results should be handled as they arrive.</td>
          </tr>
        </tbody>
      </table>

      <div className="callout">
        <p>
          Callout: the loop variable is per iteration since Go 1.22, so capturing <code>i</code> and{" "}
          <code>url</code> in the closure is safe.
        </p>
      </div>

      <h3>Steps</h3>
      <ol>
        <li>Allocate one slot per URL.</li>
        <li>Start one goroutine per URL.</li>
        <li>Wait, then check the errors in order.</li>
      </ol>
      <ul>
        <li>Results keep the order of the input.</li>
        <li>The context cancels every request at once.</li>
      </ul>

      <div className="generated">
        <span className="generated-label">generated</span>
        <p>Machine-made content, such as a later OCR fix, is marked like this.</p>
      </div>
    </article>
  );
}

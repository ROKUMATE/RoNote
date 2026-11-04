import * as Tabs from "@radix-ui/react-tabs";
import { Fragment } from "react";
import type { IrBlock, IrCell, IrInline } from "../api/schema";
import { CodeBlock } from "../ui/CodeBlock";
import { useHighlight } from "./useHighlight";

/*
 * Read-only rendering of IR blocks, for what the editor shows whole: the
 * panels of tabs, table cells and image captions.
 */

/** Where a link goes: anchors stay in the document, the rest open a new tab. */
function linkProps(link: NonNullable<IrInline["link"]>) {
  return link.kind === "anchor"
    ? { href: link.href }
    : { href: link.href, target: "_blank", rel: "noopener noreferrer" };
}

function Inlines({ runs }: { runs: readonly IrInline[] | null | undefined }) {
  return (runs ?? []).map((run, i) => {
    let node: React.ReactNode = run.text;
    for (const mark of run.marks ?? []) {
      node = wrap(mark, node);
    }
    if (run.link) node = <a {...linkProps(run.link)}>{node}</a>;
    // biome-ignore lint/suspicious/noArrayIndexKey: runs have no identity, and never reorder.
    return <span key={i}>{node}</span>;
  });
}

function wrap(mark: NonNullable<IrInline["marks"]>[number], node: React.ReactNode) {
  switch (mark) {
    case "bold":
      return <strong>{node}</strong>;
    case "italic":
      return <em>{node}</em>;
    case "code":
      return <code>{node}</code>;
    case "kbd":
      return <kbd>{node}</kbd>;
    case "strike":
      return <s>{node}</s>;
    case "sub":
      return <sub>{node}</sub>;
    case "sup":
      return <sup>{node}</sup>;
  }
}

function Blocks({ blocks }: { blocks: readonly IrBlock[] }) {
  return blocks.map((b) => <Block key={b.id} block={b} />);
}

export function Block({ block }: { block: IrBlock }) {
  switch (block.kind) {
    case "heading": {
      const Tag = `h${Math.min(Math.max(block.level ?? 2, 1), 6)}` as "h2";
      return (
        <Tag id={block.anchor}>
          <Inlines runs={block.content} />
        </Tag>
      );
    }
    case "paragraph":
      return (
        <p>
          <Inlines runs={block.content} />
        </p>
      );
    case "list": {
      const items = (block.items ?? []).map((item, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: items have no id of their own.
        <li key={i}>
          <Blocks blocks={item.children} />
        </li>
      ));
      return block.ordered ? <ol>{items}</ol> : <ul>{items}</ul>;
    }
    case "quote":
      return (
        <blockquote>
          <Blocks blocks={block.children ?? []} />
        </blockquote>
      );
    case "callout":
      return (
        <aside className="callout" data-variant={block.variant}>
          {block.title && <p className="callout-title">{block.title}</p>}
          <Blocks blocks={block.children ?? []} />
        </aside>
      );
    case "code":
      return <HighlightedCode block={block} />;
    case "table":
      return <Table block={block} />;
    case "tabs":
      return <TabGroup block={block} />;
    case "image":
      return <Image block={block} />;
    case "divider":
      return <hr />;
  }
}

function Table({ block }: { block: IrBlock }) {
  const row = (cells: IrCell[], Cell: "th" | "td") =>
    cells.map((runs, i) => (
      // biome-ignore lint/suspicious/noArrayIndexKey: cells are positional.
      <Cell key={i}>
        <Inlines runs={runs} />
      </Cell>
    ));
  return (
    // biome-ignore lint/a11y/noNoninteractiveTabindex: a wide table scrolls, so it must be reachable by keyboard.
    <div className="table-scroll" tabIndex={0}>
      <table>
        {block.head && block.head.length > 0 && (
          <thead>
            {block.head.map((cells, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: rows are positional.
              <tr key={i}>{row(cells, "th")}</tr>
            ))}
          </thead>
        )}
        <tbody>
          {(block.rows ?? []).map((cells, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: rows are positional.
            <tr key={i}>{row(cells, "td")}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Tabs follow the WAI-ARIA tabs pattern, through Radix Tabs. */
function TabGroup({ block }: { block: IrBlock }) {
  const items = block.items ?? [];
  return (
    <Tabs.Root defaultValue="0" className="tabs">
      <Tabs.List className="tabs-list">
        {items.map((item, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: tabs are positional.
          <Tabs.Trigger key={i} value={String(i)} className="tabs-trigger">
            {item.label}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      {items.map((item, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: tabs are positional.
        <Tabs.Content key={i} value={String(i)} className="tabs-panel">
          <Blocks blocks={item.children} />
        </Tabs.Content>
      ))}
    </Tabs.Root>
  );
}

function Image({ block }: { block: IrBlock }) {
  return (
    <figure>
      <img src={block.src} alt={block.alt ?? ""} loading="lazy" referrerPolicy="no-referrer" />
      {block.caption && block.caption.length > 0 && (
        <figcaption>
          <Inlines runs={block.caption} />
        </figcaption>
      )}
    </figure>
  );
}

/** A code block, highlighted once it nears the viewport. */
function HighlightedCode({ block }: { block: IrBlock }) {
  const code = block.text ?? "";
  const { ref, lines } = useHighlight(code, block.lang);
  return (
    <CodeBlock
      ref={ref}
      code={code}
      lang={block.lang}
      title={block.title}
      titleHref={block.title_link?.href}
    >
      {lines?.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: lines are positional.
        <Fragment key={i}>
          {i > 0 && "\n"}
          {line.map((token, j) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: tokens are positional.
            <span key={j} style={{ color: token.color }}>
              {token.content}
            </span>
          ))}
        </Fragment>
      ))}
    </CodeBlock>
  );
}

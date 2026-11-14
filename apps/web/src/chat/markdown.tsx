import { Link } from "@tanstack/react-router";
import { Fragment, type ReactNode, useId, useState } from "react";
import type { Citation, MessageSegment } from "../api/schema";

/*
 * An answer's Markdown, rendered as elements, never as HTML (ADR 0012):
 * paragraphs, lists, fenced and inline code, bold and italics. Headings and
 * quotes read as text, an image as its alt text, raw HTML as the text it
 * is. A link stays a link only to a document the answer cites; any other is
 * its label. Citation numbers sit after the words of the segment they back.
 */

// Private-use characters, which no answer's text means anything by, mark
// where a citation goes.
const open = "";
const close = "";
const marker = /(\d+)/g;

/** The answer as one Markdown text, a marker after each cited segment's words. */
export function withMarkers(segments: readonly MessageSegment[]): string {
  return segments
    .map(({ text, citations }) => {
      if (citations.length === 0) return text;
      const end = text.length - (/\s*$/.exec(text)?.[0].length ?? 0);
      return `${text.slice(0, end)}${citations.map((n) => `${open}${n}${close}`).join("")}${text.slice(end)}`;
    })
    .join("");
}

type Block =
  | { kind: "p"; text: string }
  | { kind: "code"; lang: string; text: string }
  | { kind: "list"; ordered: boolean; items: string[] };

/** Splits Markdown into the blocks the answer renders. */
export function blocks(markdown: string): Block[] {
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  const out: Block[] = [];
  let para: string[] = [];
  let list: { ordered: boolean; items: string[] } | undefined;
  const endPara = () => {
    if (para.length > 0) out.push({ kind: "p", text: para.join(" ") });
    para = [];
  };
  const endList = () => {
    if (list) out.push({ kind: "list", ...list });
    list = undefined;
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    const fence = /^\s*(`{3,}|~{3,})\s*([\w+#.-]*)\s*$/.exec(line);
    if (fence) {
      endPara();
      endList();
      const code: string[] = [];
      for (i++; i < lines.length && !(lines[i] ?? "").trim().startsWith(fence[1] ?? "```"); i++) {
        code.push(lines[i] ?? "");
      }
      out.push({ kind: "code", lang: fence[2] ?? "", text: code.join("\n") });
      continue;
    }
    if (line.trim() === "") {
      endPara();
      endList();
      continue;
    }
    const item = /^\s*(?:[-*+]|(\d+)[.)])\s+(.*)$/.exec(line);
    if (item) {
      endPara();
      const ordered = item[1] !== undefined;
      if (list && list.ordered !== ordered) endList();
      list ??= { ordered, items: [] };
      list.items.push(item[2] ?? "");
      continue;
    }
    const text = line.replace(/^\s*(#{1,6}\s+|>\s?)/, "").trim();
    if (list && /^\s/.test(line)) {
      list.items[list.items.length - 1] += ` ${text}`;
      continue;
    }
    endList();
    para.push(text);
  }
  endPara();
  endList();
  return out;
}

/** How a rendered answer treats citations and links. */
type Context = {
  citations: ReadonlyMap<number, Citation>;
  /** The documents the answer cites: the only ones a link may lead to. */
  documents: ReadonlySet<string>;
  chip: (citation: Citation) => ReactNode;
};

const inline = new RegExp(
  [
    String.raw`(?<n>\d+)`,
    "`(?<code>[^`]+)`",
    String.raw`!\[(?<alt>[^\]]*)\]\([^)]*\)`,
    String.raw`\[(?<label>[^\]]+)\]\((?<href>[^)\s]+)\)`,
    String.raw`\*\*(?<strong>.+?)\*\*`,
    String.raw`(?<![\w*])\*(?<em>[^*\s](?:[^*]*[^*\s])?)\*(?![\w*])`,
    String.raw`(?<!\w)_(?<emu>[^_\s](?:[^_]*[^_\s])?)_(?!\w)`,
  ].join("|"),
  "g",
);

const documentPath = /^\/doc\/([0-9a-f-]{36})(?:\?block=([\w-]+))?$/;

function inlines(text: string, ctx: Context): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(inline)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    last = m.index + m[0].length;
    const g = m.groups ?? {};
    const key = out.length;
    if (g.n !== undefined) {
      const citation = ctx.citations.get(Number(g.n));
      // A number with no valid citation behind it renders nothing.
      if (citation) out.push(<Fragment key={key}>{ctx.chip(citation)}</Fragment>);
    } else if (g.code !== undefined) {
      out.push(
        <code key={key} className="rounded-sm bg-paper-2 px-1 font-mono text-code">
          {g.code.replace(marker, "")}
        </code>,
      );
    } else if (g.alt !== undefined) {
      out.push(g.alt);
    } else if (g.label !== undefined && g.href !== undefined) {
      const target = documentPath.exec(g.href);
      const id = target?.[1];
      if (id && ctx.documents.has(id)) {
        out.push(
          <Link
            key={key}
            to="/doc/$id"
            params={{ id }}
            search={target?.[2] ? { block: target[2] } : {}}
            className="underline underline-offset-2"
          >
            {inlines(g.label, ctx)}
          </Link>,
        );
      } else {
        out.push(<Fragment key={key}>{inlines(g.label, ctx)}</Fragment>);
      }
    } else if (g.strong !== undefined) {
      out.push(<strong key={key}>{inlines(g.strong, ctx)}</strong>);
    } else {
      out.push(<em key={key}>{inlines(g.em ?? g.emu ?? "", ctx)}</em>);
    }
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/**
 * An answer's segments as Markdown, its citations as numbered chips that
 * land on their blocks. Hovering or focusing a chip shows what it cites.
 */
export function Markdown({
  segments,
  citations,
  onLand,
}: {
  segments: readonly MessageSegment[];
  citations: readonly Citation[];
  onLand: (citation: Citation) => void;
}) {
  const [shown, setShown] = useState<number>();
  const id = useId();
  const byNumber = new Map(citations.map((c) => [c.n, c]));
  const ctx: Context = {
    citations: byNumber,
    documents: new Set(citations.map((c) => c.document_id)),
    chip: (c) => (
      <button
        type="button"
        aria-label={`Source ${c.n}: ${c.title}`}
        aria-describedby={`${id}-${c.n}`}
        onClick={() => onLand(c)}
        onMouseEnter={() => setShown(c.n)}
        onMouseLeave={() => setShown(undefined)}
        onFocus={() => setShown(c.n)}
        onBlur={() => setShown(undefined)}
        className="mx-0.5 inline-flex min-w-5 items-center justify-center rounded-sm border border-rule px-1 align-baseline font-sans text-ink-2 text-ui leading-tight hover:bg-paper-2 hover:text-ink"
      >
        {c.n}
      </button>
    ),
  };
  const preview = shown === undefined ? undefined : byNumber.get(shown);
  return (
    <>
      {blocks(withMarkers(segments)).map((block, i) => {
        switch (block.kind) {
          case "code":
            return (
              <pre
                // biome-ignore lint/suspicious/noArrayIndexKey: an answer's blocks never move.
                key={i}
                className="my-2 overflow-x-auto rounded-sm bg-paper-2 p-2 font-mono text-code"
              >
                <code>{block.text.replace(marker, "")}</code>
              </pre>
            );
          case "list": {
            const List = block.ordered ? "ol" : "ul";
            return (
              <List
                // biome-ignore lint/suspicious/noArrayIndexKey: an answer's blocks never move.
                key={i}
                className={`my-2 space-y-1 pl-5 ${block.ordered ? "list-decimal" : "list-disc"}`}
              >
                {block.items.map((item, j) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: a list's items never move.
                  <li key={j}>{inlines(item, ctx)}</li>
                ))}
              </List>
            );
          }
          case "p":
            return (
              // biome-ignore lint/suspicious/noArrayIndexKey: an answer's blocks never move.
              <p key={i} className="my-2">
                {inlines(block.text, ctx)}
              </p>
            );
        }
        return null;
      })}
      {citations.map((c) => (
        <span key={c.n} id={`${id}-${c.n}`} className="sr-only">
          {c.cited_text}
        </span>
      ))}
      {preview && (
        <div className="mt-2 rounded-sm border border-rule bg-paper-2 px-3 py-2 text-ui">
          <p className="font-semibold text-ink-2">{preview.title}</p>
          <p className="mt-1 line-clamp-6 whitespace-pre-wrap">{preview.cited_text}</p>
        </div>
      )}
    </>
  );
}

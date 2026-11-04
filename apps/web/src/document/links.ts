import type { IrBlock, IrInline, IrLink } from "../api/schema";

/*
 * Same-domain links (ADR 0006): a link to another page of the source site
 * is internal when the user has that page as a document, and importable
 * when they have not imported it yet.
 */

/** What following a same-domain link does. */
export type LinkStatus =
  | { state: "internal"; documentId: string }
  | { state: "import" }
  | { state: "unknown" };

/** The resolve answers by href: the document for it, or null for none. */
export type LinkTargets = ReadonlyMap<string, string | null>;

/** Every same-domain href in blocks, nested ones included, each once. */
export function sameDomainHrefs(blocks: readonly IrBlock[]): string[] {
  const hrefs = new Set<string>();
  const runs = (list: readonly IrInline[] | null | undefined) => {
    for (const run of list ?? []) {
      if (run.link?.kind === "same_domain") hrefs.add(run.link.href);
    }
  };
  const walk = (list: readonly IrBlock[]) => {
    for (const b of list) {
      runs(b.content);
      runs(b.caption);
      for (const row of [...(b.head ?? []), ...(b.rows ?? [])]) {
        for (const cell of row) runs(cell);
      }
      walk(b.children ?? []);
      for (const item of b.items ?? []) walk(item.children);
    }
  };
  walk(blocks);
  return [...hrefs];
}

/** Where a same-domain link leads, once its target is known. */
export function linkStatus(href: string, targets: LinkTargets | undefined): LinkStatus {
  if (!targets?.has(href)) return { state: "unknown" };
  const documentId = targets.get(href);
  return documentId ? { state: "internal", documentId } : { state: "import" };
}

/** href without its fragment, and the fragment without its #. */
export function splitFragment(href: string): { url: string; fragment: string } {
  const at = href.indexOf("#");
  return at < 0
    ? { url: href, fragment: "" }
    : { url: href.slice(0, at), fragment: href.slice(at + 1) };
}

/** The id of the text that says what following a same-domain link does. */
export const followHelpId = "link-follow-help";

/**
 * A link's HTML attributes, the same in the editor and in locked blocks.
 * External links open a new tab; same-domain links are followed by
 * RoNote, so they carry no target and point at the follow help.
 */
export function linkAttributes(href: string, kind: IrLink["kind"]): Record<string, string> {
  switch (kind) {
    case "anchor":
      return { href, "data-kind": kind };
    case "same_domain":
      return { href, "data-kind": kind, "aria-describedby": followHelpId };
    case "external":
      return { href, "data-kind": kind, target: "_blank", rel: "noopener noreferrer" };
  }
}

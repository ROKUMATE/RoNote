/*
 * What a click on a link in a document does (ADR 0006):
 * - an anchor moves within the document;
 * - an external link opens in a new tab;
 * - a same-domain link is followed in RoNote: opened if the page is in the
 *   library, imported first if not;
 * - Cmd/Ctrl-click and middle-click always open the original in a new tab,
 *   the way out of RoNote's handling.
 */

export type LinkAction =
  | { do: "anchor"; fragment: string }
  | { do: "open"; href: string }
  | { do: "follow"; href: string };

/** What event on link should do, or undefined to leave it to the browser. */
export function linkAction(link: Element, event: MouseEvent): LinkAction | undefined {
  const href = link.getAttribute("href");
  if (!href) return undefined;
  const kind = link.getAttribute("data-kind");
  const toOriginal = event.metaKey || event.ctrlKey || event.button === 1;
  if (kind === "anchor") {
    return toOriginal ? undefined : { do: "anchor", fragment: href.slice(1) };
  }
  if (toOriginal || kind !== "same_domain") return { do: "open", href };
  return { do: "follow", href };
}

/** Opens href in a new tab, with nothing of RoNote's page passed along. */
export function openOriginal(href: string) {
  window.open(href, "_blank", "noopener,noreferrer");
}

import type { JSONContent } from "@tiptap/core";
import type { UserBlockKind } from "../api/schema";
import { uuidv7 } from "./uuid";

/**
 * The blocks a user can add, in the spec's UserBlockKind order, with the
 * label the block menu shows.
 */
export const userBlockLabels: Record<UserBlockKind, string> = {
  paragraph: "Text",
  heading: "Heading",
  list: "List",
  quote: "Quote",
  callout: "Callout",
  divider: "Divider",
};

export const userBlockKinds = Object.keys(userBlockLabels) as UserBlockKind[];

const emptyParagraph = (): JSONContent => ({ type: "paragraph", attrs: { blockId: uuidv7() } });

/** A new, empty user block of kind, as editor JSON. */
export function newUserBlock(kind: UserBlockKind): JSONContent {
  const attrs = { blockId: uuidv7(), origin: "user" };
  switch (kind) {
    case "paragraph":
      return { type: "paragraph", attrs };
    case "heading":
      return { type: "heading", attrs: { ...attrs, level: 2 } };
    case "list":
      return { type: "list", attrs, content: [{ type: "listItem", content: [emptyParagraph()] }] };
    case "quote":
      return { type: "blockquote", attrs, content: [emptyParagraph()] };
    case "callout":
      return { type: "callout", attrs: { ...attrs, variant: "note" }, content: [emptyParagraph()] };
    case "divider":
      return {
        type: "divider",
        attrs: { ...attrs, block: { id: attrs.blockId, kind: "divider" } },
      };
  }
}

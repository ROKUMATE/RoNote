import { expect, test } from "vitest";
import { linkAction } from "./follow";

function link(href: string, kind: string) {
  const a = document.createElement("a");
  a.setAttribute("href", href);
  a.setAttribute("data-kind", kind);
  return a;
}

const click = (init: MouseEventInit = {}) => new MouseEvent("click", { button: 0, ...init });

test.each([
  [
    "a plain click on a same-domain link follows it",
    link("https://d.example/b#usage", "same_domain"),
    click(),
    { do: "follow", href: "https://d.example/b#usage" },
  ],
  [
    "Cmd-click opens the original",
    link("https://d.example/b", "same_domain"),
    click({ metaKey: true }),
    { do: "open", href: "https://d.example/b" },
  ],
  [
    "Ctrl-click opens the original",
    link("https://d.example/b", "same_domain"),
    click({ ctrlKey: true }),
    { do: "open", href: "https://d.example/b" },
  ],
  [
    "a middle click opens the original",
    link("https://d.example/b", "same_domain"),
    new MouseEvent("auxclick", { button: 1 }),
    { do: "open", href: "https://d.example/b" },
  ],
  [
    "an external link opens in a new tab",
    link("https://o.example/", "external"),
    click(),
    { do: "open", href: "https://o.example/" },
  ],
  [
    "an anchor moves within the document",
    link("#setup", "anchor"),
    click(),
    { do: "anchor", fragment: "setup" },
  ],
  [
    "a modified click on an anchor is left alone",
    link("#setup", "anchor"),
    click({ metaKey: true }),
    undefined,
  ],
] as const)("%s", (_name, a, event, want) => {
  expect(linkAction(a, event)).toEqual(want);
});

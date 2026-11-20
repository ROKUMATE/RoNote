import { afterEach, expect, test } from "vitest";
import { configuredOrigin } from "./origins";

afterEach(() => {
  document.head.replaceChildren();
});

function meta(name: string, content: string) {
  const tag = document.createElement("meta");
  tag.name = name;
  tag.content = content;
  document.head.append(tag);
}

test("an origin comes from its meta tag, without a path", () => {
  meta("ronote-runner-url", "https://ronote-run.test/");
  meta("ronote-app-url", "https://ronote.test");
  expect(configuredOrigin("ronote-runner-url")).toBe("https://ronote-run.test");
  expect(configuredOrigin("ronote-app-url")).toBe("https://ronote.test");
});

// An unfilled template or a missing tag must never become a relative URL,
// which would load the app itself in the runner's frame.
test("a missing, unfilled or non-web value names no origin", () => {
  expect(configuredOrigin("ronote-runner-url")).toBe("");
  for (const content of ['{{env "RONOTE_RUNNER_URL"}}', "", "javascript:alert(1)", "/runner"]) {
    document.head.replaceChildren();
    meta("ronote-runner-url", content);
    expect(configuredOrigin("ronote-runner-url"), content).toBe("");
  }
});

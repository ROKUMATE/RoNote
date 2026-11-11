import { screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { signInErrorMessages } from "../api/messages";
import { stubApi } from "../test/api";
import { renderApp } from "../test/render";

test("offers GitHub, coming back to return_to", async () => {
  renderApp("/login?return_to=%2Fdoc%2Fx");

  const link = await screen.findByRole("link", { name: "Continue with GitHub" });
  expect(link.getAttribute("href")).toBe("/api/v1/auth/github/start?return_to=%2Fdoc%2Fx");
  expect(screen.getByRole("heading", { level: 1, name: "RoNote" })).toBeDefined();
  expect(screen.queryByRole("alert")).toBeNull();
});

test("is outside the app's frame, and asks the API nothing", async () => {
  const requests = stubApi({});

  renderApp("/login");

  await screen.findByRole("link", { name: "Continue with GitHub" });
  expect(screen.queryByRole("banner")).toBeNull();
  expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  expect(requests).toEqual([]);
});

test.each(Object.entries(signInErrorMessages))(
  "says why sign-in failed: %s",
  async (code, sentence) => {
    renderApp(`/login?error=${code}`);

    expect((await screen.findByRole("alert")).textContent).toBe(sentence);
  },
);

test("ignores what it doesn't know", async () => {
  renderApp("/login?error=nonsense&return_to=https%3A%2F%2Fevil.test");

  const link = await screen.findByRole("link", { name: "Continue with GitHub" });
  expect(screen.queryByRole("alert")).toBeNull();
  expect(link.getAttribute("href")).toBe("/api/v1/auth/github/start?return_to=%2F");
});

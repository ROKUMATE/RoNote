import { type ByRoleOptions, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { json, stubApi } from "../test/api";
import { renderApp } from "../test/render";

test("renders its landmarks", async () => {
  stubApi({ "GET /documents": () => json({ documents: [] }) });
  renderApp("/");
  await screen.findByRole("main");

  const landmarks: [string, ByRoleOptions?][] = [
    ["banner"],
    ["navigation", { name: "Main" }],
    ["main"],
    ["complementary", { name: "Notes" }],
  ];
  for (const [role, options] of landmarks) {
    expect(screen.queryByRole(role, options), role).not.toBeNull();
  }
});

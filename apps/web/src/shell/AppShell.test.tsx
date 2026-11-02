import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { type ByRoleOptions, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { createAppRouter } from "../router";

test("renders its landmarks", async () => {
  render(<RouterProvider router={createAppRouter(createMemoryHistory())} />);
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

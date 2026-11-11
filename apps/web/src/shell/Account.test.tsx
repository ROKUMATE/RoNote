import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { replaceNavigation } from "../api/session";
import { apiError, json, me, stubApi } from "../test/api";
import { renderApp } from "../test/render";

let navigate: ReturnType<typeof vi.fn<(url: string) => void>>;

beforeEach(() => {
  navigate = vi.fn<(url: string) => void>();
  replaceNavigation(navigate);
});

const library = { "GET /documents": () => json({ documents: [] }) };

/**
 * Opens the account menu, as a click on the user's name does. jsdom doesn't
 * open popovers, so the menu's items stay hidden to role queries, which pass
 * hidden: true; Playwright covers the popover itself.
 */
async function openMenu() {
  fireEvent.click(await screen.findByRole("button", { name: me.display_name }));
}

test("shows who is signed in, with no avatar", async () => {
  stubApi(library);
  renderApp("/");

  const button = await screen.findByRole("button", { name: me.display_name });
  await openMenu();

  expect(button.querySelector("img")).toBeNull();
  expect(screen.getByText(`Signed in as ${me.login}`)).toBeDefined();
});

test.each([
  ["Sign out", "POST /auth/logout"],
  ["Sign out everywhere", "POST /auth/logout-all"],
])("%s ends the session and goes to the login page", async (name, route) => {
  const requests = stubApi({ ...library, [route]: () => new Response(null, { status: 204 }) });
  renderApp("/");
  await openMenu();

  fireEvent.click(await screen.findByRole("button", { name, hidden: true }));

  await waitFor(() => expect(navigate).toHaveBeenCalledExactlyOnceWith("/login"));
  expect(requests.map((r) => `${r.method} ${new URL(r.url).pathname}`)).toContain(
    `${route.split(" ")[0]} /api/v1${route.split(" ")[1]}`,
  );
});

test("deleting the account needs the login typed, then goes", async () => {
  const requests = stubApi({ ...library, "DELETE /me": () => new Response(null, { status: 204 }) });
  renderApp("/");
  await openMenu();
  fireEvent.click(await screen.findByRole("button", { name: "Delete account…", hidden: true }));
  const dialog = screen.getByRole("dialog", { name: "Delete your account?" });
  const remove = screen.getByRole("button", { name: "Delete account" });
  expect(dialog.textContent).toContain("documents");
  expect(dialog.textContent).toContain("highlight");

  const input = screen.getByLabelText(/Type your GitHub login/);
  fireEvent.change(input, { target: { value: "someone-else" } });
  expect(remove).toHaveProperty("disabled", true);
  fireEvent.change(input, { target: { value: "ADA" } });
  expect(remove).toHaveProperty("disabled", false);
  fireEvent.click(remove);

  await waitFor(() => expect(navigate).toHaveBeenCalledExactlyOnceWith("/login"));
  const deleted = requests.find((r) => r.method === "DELETE");
  expect(await deleted?.json()).toEqual({ confirm: "ADA" });
});

test("a refused deletion says why and stays", async () => {
  stubApi({ ...library, "DELETE /me": () => apiError(500, "internal_error") });
  renderApp("/");
  await openMenu();
  fireEvent.click(await screen.findByRole("button", { name: "Delete account…", hidden: true }));
  fireEvent.change(screen.getByLabelText(/Type your GitHub login/), { target: { value: "ada" } });

  fireEvent.click(screen.getByRole("button", { name: "Delete account" }));

  expect((await screen.findByRole("alert")).textContent).toContain("Something went wrong");
  expect(navigate).not.toHaveBeenCalled();
});

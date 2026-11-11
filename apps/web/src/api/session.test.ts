import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { apiError, stubApi } from "../test/api";
import { api } from "./client";
import { leave, replaceNavigation, signInAgain } from "./session";

let navigate: ReturnType<typeof vi.fn<(url: string) => void>>;

beforeEach(() => {
  navigate = vi.fn<(url: string) => void>();
  replaceNavigation(navigate);
});

afterEach(() => {
  history.replaceState(null, "", "/");
});

test("signing in again comes back to where the reader was", () => {
  history.replaceState(null, "", "/doc/x?tab=notes#h-intro");

  signInAgain();

  expect(navigate).toHaveBeenCalledExactlyOnceWith(
    "/login?return_to=%2Fdoc%2Fx%3Ftab%3Dnotes%23h-intro",
  );
});

test("many requests told to sign in again leave once", () => {
  history.replaceState(null, "", "/doc/x");

  signInAgain();
  signInAgain();
  leave("/login");

  expect(navigate).toHaveBeenCalledOnce();
});

test("the login page never sends to itself", () => {
  history.replaceState(null, "", "/login?return_to=%2Fdoc%2Fx");

  signInAgain();

  expect(navigate).not.toHaveBeenCalled();
});

test("any API call answered 401 goes to sign in", async () => {
  history.replaceState(null, "", "/doc/x");
  stubApi({ "GET /documents": () => apiError(401, "unauthenticated") });

  const { response } = await api.GET("/documents");

  expect(response.status).toBe(401);
  expect(navigate).toHaveBeenCalledExactlyOnceWith("/login?return_to=%2Fdoc%2Fx");
});

test("other failures don't", async () => {
  stubApi({ "GET /documents": () => apiError(500, "internal_error") });

  await api.GET("/documents");

  expect(navigate).not.toHaveBeenCalled();
});

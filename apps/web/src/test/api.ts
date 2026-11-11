import { vi } from "vitest";

type Handler = (request: Request) => Response | Promise<Response>;

/** A JSON response, as the API sends it. */
export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** An error envelope response. */
export function apiError(status: number, code: string): Response {
  return json({ error: { code, message: code, request_id: "test" } }, status);
}

/** The signed-in user stubApi answers GET /me with, unless told otherwise. */
export const me = {
  id: "0192f1c4-0000-7000-8000-0000000000aa",
  display_name: "Ada Lovelace",
  email: "ada@example.com",
  login: "ada",
};

/**
 * Stubs fetch with handlers keyed by method and API path, such as
 * "GET /documents", and returns the requests it received. A request no
 * handler matches fails the test with its key. GET /me answers with me,
 * unrecorded, when routes leave it out: the shell asks on every page.
 */
export function stubApi(routes: Record<string, Handler>): Request[] {
  const requests: Request[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    const key = `${request.method} ${new URL(request.url).pathname.replace(/^\/api\/v1/, "")}`;
    if (key === "GET /me" && !routes[key]) return json(me);
    requests.push(request);
    const handler = routes[key];
    if (!handler) throw new Error(`no API stub for ${key}`);
    return handler(request);
  });
  return requests;
}

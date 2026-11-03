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

/**
 * Stubs fetch with handlers keyed by method and API path, such as
 * "GET /documents", and returns the requests it received. A request no
 * handler matches fails the test with its key.
 */
export function stubApi(routes: Record<string, Handler>): Request[] {
  const requests: Request[] = [];
  vi.stubGlobal("fetch", async (request: Request) => {
    requests.push(request);
    const key = `${request.method} ${new URL(request.url).pathname.replace(/^\/api\/v1/, "")}`;
    const handler = routes[key];
    if (!handler) throw new Error(`no API stub for ${key}`);
    return handler(request);
  });
  return requests;
}

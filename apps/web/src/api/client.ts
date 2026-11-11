import createClient from "openapi-fetch";
import type { ErrorBody, paths } from "./schema";
import { signInAgain } from "./session";

/** Where the API lives; in dev, Vite proxies it to the Go server. */
export const apiBase = "/api/v1";

/**
 * The API client, typed from api/openapi.yaml. The base URL is absolute so
 * requests also build where a relative URL has no base, as in Node; fetch is
 * looked up on every call, so tests can stub it. A 401 means the session is
 * gone, whichever call got it: the browser goes to sign in.
 */
export const api = createClient<paths>({
  baseUrl: new URL(apiBase, location.origin).href,
  fetch: async (request) => {
    const response = await globalThis.fetch(request);
    if (response.status === 401) signInAgain();
    return response;
  },
});

/** A failed API call: its HTTP status, and the error envelope if it sent one. */
export class ApiError extends Error {
  readonly status: number;
  readonly body: ErrorBody | undefined;

  constructor(status: number, body: ErrorBody | undefined) {
    super(body ? `${body.code}: ${body.message}` : `HTTP ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

/**
 * The ErrorBody inside an error response, or undefined when the body is not
 * the envelope, as when a proxy answers instead of the API.
 */
function errorBody(error: unknown): ErrorBody | undefined {
  if (typeof error === "object" && error !== null && "error" in error) {
    return (error as { error: ErrorBody }).error;
  }
  return undefined;
}

/** Throws the ApiError for a failed response; returns the data otherwise. */
export function dataOrThrow<T>({
  data,
  error,
  response,
}: {
  data?: T;
  error?: unknown;
  response: Response;
}): T {
  if (!response.ok || data === undefined) {
    throw new ApiError(response.status, errorBody(error));
  }
  return data;
}

/** Throws the ApiError for a failed response that has no body when it succeeds. */
export function okOrThrow({ error, response }: { error?: unknown; response: Response }): void {
  if (!response.ok) throw new ApiError(response.status, errorBody(error));
}

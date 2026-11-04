import { ApiError } from "./client";
import type { ErrorCode, ImportErrorCode } from "./schema";

/** Why an import failed, one plain sentence per code. */
export const importErrorMessages: Record<ImportErrorCode, string> = {
  invalid_url: "That link isn't a web address RoNote can open.",
  blocked_address: "That link points to a private network, which RoNote doesn't fetch from.",
  disallowed_by_robots: "This site doesn't allow RoNote to fetch its pages.",
  too_large: "That page is larger than RoNote imports (5 MB).",
  unsupported_type: "That link isn't a web page. PDF import comes later.",
  too_many_redirects: "That link redirects too many times.",
  not_found: "The site says that page doesn't exist.",
  http_error: "The site answered with an error.",
  extract_failed: "RoNote couldn't read the content of that page.",
  timeout: "The site took too long to answer.",
  rate_limited: "The site asked RoNote to slow down. Try again in a few minutes.",
  upstream_error: "The site is having trouble right now. Try again later.",
  robots_unavailable: "The site couldn't say whether RoNote may fetch its pages. Try again later.",
  network_error: "RoNote couldn't connect to the site.",
  internal_error: "Something went wrong on RoNote's side. Try again.",
};

/** Why the API refused a request, one plain sentence per code. */
export const requestErrorMessages: Record<ErrorCode, string> = {
  invalid_request: "RoNote couldn't read that request. Try again.",
  invalid_url: "That isn't a web address. Paste a link that starts with https://.",
  unsupported_scheme: "Only http and https links can be imported.",
  url_too_long: "That link is longer than 2048 characters.",
  ip_literal_host: "Use the site's name, not an IP address.",
  invalid_parameter: "RoNote couldn't read that request. Try again.",
  invalid_cursor: "That page of your library has expired. Reload to start again.",
  not_found: "That isn't in your library.",
  method_not_allowed: "RoNote couldn't read that request. Try again.",
  version_conflict: "This document changed in another tab.",
  invalid_content: "RoNote couldn't save that change. Reload to continue.",
  unknown_source_block: "Part of this document no longer matches its source. Reload to continue.",
  source_block_modified: "Code, tables and tabs from the page can't be edited.",
  block_kind_not_allowed:
    "Notes can only hold text, headings, lists, quotes, callouts and dividers.",
  unauthenticated: "You're signed out. Sign in and try again.",
  internal_error: "Something went wrong on RoNote's side. Try again.",
};

/** The API did not answer, or something else answered for it. */
const unreachableMessage = "RoNote's server isn't answering. Try again in a moment.";

/** The progress stream gave up before the import finished. */
export const lostMessage =
  "Lost track of this import. Reload the library to see whether it finished.";

/** Why a request failed, in one sentence. */
export function requestErrorMessage(error: unknown): string {
  return error instanceof ApiError && error.body
    ? requestErrorMessages[error.body.code]
    : unreachableMessage;
}

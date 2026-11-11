import { ApiError } from "./client";
import type { ErrorCode, ImportErrorCode, SignInErrorCode } from "./schema";

/** Why signing in failed, one plain sentence per code. */
export const signInErrorMessages: Record<SignInErrorCode, string> = {
  denied: "GitHub didn't sign you in. Try again.",
  state_mismatch: "That sign-in took too long or started in another tab. Try again.",
  no_verified_email:
    "Your GitHub account needs a verified primary email. Add one on GitHub, then try again.",
  provider_unavailable: "GitHub isn't answering right now. Try again in a few minutes.",
};

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
  unknown_block: "That text can't be highlighted.",
  quote_mismatch: "The text changed before the highlight was saved. Try again.",
  annotation_limit: "This document already has 2000 highlights, the most it can hold.",
  unauthenticated: "You're signed out. Sign in and try again.",
  cross_origin: "RoNote only accepts changes from its own pages. Reload RoNote and try again.",
  rate_limited: "That's a lot of requests at once. Wait a minute and try again.",
  quota_exceeded:
    "You've reached an import limit: 60 pages a day, or 500 documents in your library.",
  body_too_large: "That change is too large to save in one go.",
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

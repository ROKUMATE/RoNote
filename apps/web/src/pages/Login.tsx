import { GithubLogoIcon } from "@phosphor-icons/react/GithubLogo";
import { signInErrorMessages } from "../api/messages";
import type { SignInErrorCode } from "../api/schema";
import { buttonClass } from "../ui/button";
import { PageTitle } from "../ui/PageTitle";

/** What /login reads from its query: why the last sign-in failed, and where to go after. */
export type LoginSearch = { error?: SignInErrorCode; return_to?: string };

/** Keeps the search params /login understands, dropping anything else. */
export function validateLoginSearch(search: Record<string, unknown>): LoginSearch {
  const { error, return_to } = search;
  return {
    ...(typeof error === "string" && error in signInErrorMessages
      ? { error: error as SignInErrorCode }
      : {}),
    ...(typeof return_to === "string" && return_to.startsWith("/") ? { return_to } : {}),
  };
}

/**
 * The sign-in page, outside the app's frame. Signing in is a link, not a
 * script: the API sends the browser to GitHub and back
 * (docs/adr/0010-sign-in-and-sessions.md).
 */
export function Login({ search }: { search: Record<string, unknown> }) {
  // The router hands a route its parents' search too: read only ours.
  const { error, return_to: returnTo } = validateLoginSearch(search);
  const start = `/api/v1/auth/github/start?return_to=${encodeURIComponent(returnTo ?? "/")}`;
  return (
    <main id="main" className="min-h-dvh px-4 py-12 sidebar:px-12 sidebar:py-24">
      <div className="max-w-[68ch]">
        <PageTitle>RoNote</PageTitle>
        <p className="mt-3 text-ink-2 text-ui-lg">
          Sign in to keep the pages you import, with your highlights and notes.
        </p>
        {error && (
          <p role="alert" className="mt-6 text-ui-lg">
            {signInErrorMessages[error]}
          </p>
        )}
        <a href={start} className={`${buttonClass} mt-6`}>
          <GithubLogoIcon size={16} aria-hidden />
          Continue with GitHub
        </a>
      </div>
    </main>
  );
}

/**
 * Leaving the app for sign-in (docs/adr/0010-sign-in-and-sessions.md). A
 * full page load, not a route change: the sign-in goes through the API and
 * GitHub, and comes back to a fresh app.
 */

let go: (url: string) => void = (url) => location.assign(url);
let leaving = false;

/** Replaces how the browser leaves, and forgets an earlier leave; for tests. */
export function replaceNavigation(navigate: (url: string) => void): void {
  go = navigate;
  leaving = false;
}

/** Leaves the app for url, once: later calls do nothing. */
export function leave(url: string): void {
  if (leaving) return;
  leaving = true;
  go(url);
}

/**
 * The API said the session is gone: off to sign in, coming back here after.
 * However many requests say so, the browser goes once, and never from the
 * login page itself.
 */
export function signInAgain(): void {
  if (location.pathname === "/login") return;
  const here = location.pathname + location.search + location.hash;
  leave(`/login?return_to=${encodeURIComponent(here)}`);
}

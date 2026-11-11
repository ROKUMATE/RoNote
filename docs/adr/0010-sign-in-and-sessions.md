# 0010: Sign-in and sessions

Accepted, 2026-11-11

## Context

Until P15 every `/api/v1` request acted as one dev user, `RONOTE_DEV_USER_EMAIL`, and staging and prod answered 401. RoNote needs real accounts before it can be deployed (P18): sign-in, sessions, a way out, and limits so one account can't exhaust the service. ADR 0008 already fixes one constraint: the session cookie is host-only, so the runner's separate site never sees it.

## Decision

- **Sign-in is GitHub OAuth**, the authorization-code flow with `state` and PKCE (S256), scopes `read:user user:email`.
  - `GET /api/v1/auth/github/start?return_to=` puts the state and the PKCE verifier in a 10-minute HttpOnly cookie and redirects (302) to the provider.
  - `GET /api/v1/auth/github/callback` compares the state with the cookie's in constant time, exchanges the code with the verifier, reads the user and their primary verified email, upserts the user and the identity, starts a session, and redirects (303) to `return_to`.
  - Failures redirect to `/login?error=<code>`:
    - `denied`: the user refused at the provider, or the provider refused the code;
    - `state_mismatch`: the state cookie is missing, expired or different;
    - `no_verified_email`: the provider reports no primary, verified email;
    - `provider_unavailable`: the provider timed out, failed, or sent too much.
  - `return_to` is a path on RoNote's own origin; anything else (`//host`, `/\host`, `https://host`, control characters) becomes `/`.
  - Provider calls time out after 5 seconds and read a bounded response.
  - Sign-in never uses a popup: COOP `same-origin` (ADR 0008) would cut `window.opener`.
- **Who a user is:** an identity, `(provider, provider_user_id)`, never an email.
  - A new identity joins an existing user only when the provider reports that user's email as the identity's primary, verified email. Otherwise a new user is created.
  - The identity keeps the GitHub login, refreshed at every sign-in. Deleting an account asks for it.
- **One sign-in path in every environment.** The dev identity (`RONOTE_DEV_USER_EMAIL` and its middleware) is gone.
  - Dev and test point the provider URLs at `cmd/fakeidp`, a fake provider serving the same four endpoints. Its sign-in page lists test users from a JSON file and has a Deny button.
  - Config refuses any provider URL other than `https://github.com` and `https://api.github.com` in staging and prod, so the fake can't be configured there.
- **Sessions live in Postgres** (`sessions`): 32 random bytes per session, of which only the SHA-256 is stored.
  - A session ends after 30 days unused or 90 days in all (`expires_at`), whichever comes first.
  - Every sign-in issues a new token and ends the session the browser came with.
  - `last_seen_at` is written at most once per 5 minutes per session.
  - Sign out ends this session; Sign out everywhere ends all of the user's.
- **The cookie** is HttpOnly, `SameSite=Lax`, `Path=/`, with no `Domain` (ADR 0008).
  - In staging and prod it is also `Secure`, named with the `__Host-` prefix, which the browser only accepts for exactly those attributes.
  - The state cookie follows the same rules. `Lax` lets both travel on the provider's top-level redirect back.
- **CSRF:** Go's `http.CrossOriginProtection` guards every `/api` route.
  - It refuses unsafe requests that `Sec-Fetch-Site` or, failing that, `Origin` shows came from another site. `RONOTE_PUBLIC_URL` is the only trusted origin, and there are no bypass patterns.
  - No CSRF tokens: every browser RoNote supports sends `Sec-Fetch-Site`.
- **Deleting an account** (`DELETE /api/v1/me`, `{confirm: "<github login>"}`) cascades to documents, imports, annotations, identities and sessions.
  - Sources, snapshots and extractions are shared between users and stay.
  - An import job already queued for the user ends when it finds its import gone: the worker treats a missing import as permanent.
- **Limits** answer 429, with `Retry-After` when waiting helps:
  - **Quotas** (`quota_exceeded`): 60 imports per user per day and 500 documents per user. They count the user's rows in Postgres, in the import's transaction, under a per-user advisory lock, so concurrent requests can't overshoot.
    - The daily quota is a rolling 24 hours; `Retry-After` is when the oldest import in it ages out.
    - The documents quota sends no `Retry-After`: waiting frees nothing.
  - **Rate limits** (`rate_limited`): 120 API requests per user per minute, the import event stream excluded, and 20 auth requests per client IP per minute.
    - They count in memory, per API process, in fixed one-minute windows. A second API process needs a shared store first (`docs/open-questions.md`).
    - The client IP is the connection's `RemoteAddr`. Trusting a proxy's headers comes with the deployment, P19.
  - Defaults live in config (`RONOTE_LIMIT_*`).
- **Every route bounds its request body**, answering 413 `body_too_large` past it:
  - 8 MiB for saving a document, whose source page may be up to 5 MB;
  - 1 MiB for re-anchoring highlights;
  - 64 KiB for every other route.

## Consequences

- Dev needs the fake provider running (`make run-idp`; `make web-dev` and `make web-e2e` start it). A dev user list includes `dev@ronote.local` as verified, so documents made under the old dev identity stay reachable.
- End-to-end tests sign in through the same flow as people do. Each Playwright worker is its own user, which also keeps workers' libraries apart.
- Staging and prod need a GitHub OAuth app per deployment, with its client ID, secret and callback URL.
- Rate limits reset when the API restarts, and are per process.

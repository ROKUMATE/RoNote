import { defineConfig, devices } from "@playwright/test";
import { stack } from "./e2e/stack";

// make web-e2e prepares an empty, migrated database and builds the Go
// binaries; Playwright then starts the rest of the stack.
const databaseURL = process.env.RONOTE_E2E_DATABASE_URL;
if (!databaseURL) {
  throw new Error("RONOTE_E2E_DATABASE_URL is not set: run the end-to-end tests with make web-e2e");
}

/** The environment, minus unset variables, plus overrides. */
function env(overrides: Record<string, string>): Record<string, string> {
  const base = Object.entries(process.env).filter(
    (entry): entry is [string, string] => entry[1] !== undefined,
  );
  return { ...Object.fromEntries(base), ...overrides };
}

// The OAuth app the API signs in with, which the fake provider accepts.
const oauthApp = {
  RONOTE_GITHUB_CLIENT_ID: "ronote-e2e",
  RONOTE_GITHUB_CLIENT_SECRET: "ronote-e2e-secret",
};

// Set here rather than inherited, so .env.dev can never point the tests at
// the dev database or the dev sign-in.
const backend = env({
  RONOTE_ENV: "test",
  RONOTE_DATABASE_URL: databaseURL,
  RONOTE_HTTP_ADDR: new URL(stack.api).host,
  RONOTE_FETCH_ALLOW_PRIVATE: "true",
  RONOTE_SOURCE_TTL: "24h",
  RONOTE_LOG_LEVEL: "info",
  RONOTE_PUBLIC_URL: stack.web,
  ...oauthApp,
  RONOTE_GITHUB_AUTHORIZE_URL: `${stack.idp}/login/oauth/authorize`,
  RONOTE_GITHUB_TOKEN_URL: `${stack.idp}/login/oauth/access_token`,
  RONOTE_GITHUB_API_URL: stack.idp,
  // The suite imports and signs in far more than a person does; Go tests
  // check the limits.
  RONOTE_LIMIT_IMPORTS_PER_DAY: "100000",
  RONOTE_LIMIT_DOCUMENTS: "100000",
  RONOTE_LIMIT_API_PER_MINUTE: "100000",
  RONOTE_LIMIT_AUTH_PER_MINUTE: "100000",
});

const stop = { signal: "SIGTERM", timeout: 5000 } as const;

export default defineConfig({
  testDir: "e2e",
  forbidOnly: Boolean(process.env.CI),
  timeout: 60_000,
  // Locally one line that updates in place, and failures in full; CI also
  // writes the HTML report, which the workflow uploads when a test fails.
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "line",
  use: { baseURL: stack.web, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      name: "fixtures",
      command: "node e2e/fixture-site.ts",
      url: `${stack.fixtures}/robots.txt`,
      reuseExistingServer: false,
    },
    {
      name: "idp",
      command: "bin/e2e/fakeidp",
      cwd: "../..",
      env: env({
        ...oauthApp,
        RONOTE_FAKEIDP_ADDR: new URL(stack.idp).host,
        RONOTE_FAKEIDP_USERS: "apps/web/e2e/fixtures/idp-users.json",
      }),
      url: `${stack.idp}/healthz`,
      reuseExistingServer: false,
      gracefulShutdown: stop,
    },
    {
      name: "api",
      command: "bin/e2e/api",
      cwd: "../..",
      env: backend,
      url: `${stack.api}/readyz`,
      reuseExistingServer: false,
      gracefulShutdown: stop,
    },
    {
      name: "worker",
      command: "bin/e2e/worker",
      cwd: "../..",
      env: backend,
      stdout: "pipe",
      wait: { stdout: /worker started/ },
      reuseExistingServer: false,
      gracefulShutdown: stop,
    },
    {
      name: "runner",
      command: `pnpm runner --port ${new URL(stack.runner).port}`,
      env: env({ RONOTE_APP_ORIGINS: stack.web }),
      url: `${stack.runner}/`,
      reuseExistingServer: false,
    },
    {
      name: "web",
      command: `pnpm dev --host 127.0.0.1 --port ${new URL(stack.web).port} --strictPort`,
      env: env({ RONOTE_API_ORIGIN: stack.api, RONOTE_RUNNER_ORIGIN: stack.runner }),
      url: stack.web,
      reuseExistingServer: false,
    },
  ],
});

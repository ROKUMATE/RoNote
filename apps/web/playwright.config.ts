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

// Set here rather than inherited, so .env.dev can never point the tests at
// the dev database or the dev user.
const backend = env({
  RONOTE_ENV: "test",
  RONOTE_DATABASE_URL: databaseURL,
  RONOTE_HTTP_ADDR: new URL(stack.api).host,
  RONOTE_FETCH_ALLOW_PRIVATE: "true",
  RONOTE_DEV_USER_EMAIL: "e2e@ronote.local",
  RONOTE_SOURCE_TTL: "24h",
  RONOTE_LOG_LEVEL: "info",
});

const stop = { signal: "SIGTERM", timeout: 5000 } as const;

export default defineConfig({
  testDir: "e2e",
  forbidOnly: Boolean(process.env.CI),
  timeout: 60_000,
  // CI also writes the HTML report, which the workflow uploads when a test fails.
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
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
      name: "web",
      command: `pnpm dev --host 127.0.0.1 --port ${new URL(stack.web).port} --strictPort`,
      env: env({ RONOTE_API_ORIGIN: stack.api }),
      url: stack.web,
      reuseExistingServer: false,
    },
  ],
});

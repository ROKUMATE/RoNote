import { createHash, createPublicKey } from "node:crypto";
import { readFileSync } from "node:fs";
import { defineConfig, devices } from "@playwright/test";

/*
 * The smoke suite (docs/adr/0014-production-topology.md): the production
 * units and Caddy config, with the fakes swapped in, at ronote.test and
 * ronote-run.test. make prod-smoke runs it once deploy/install.sh has
 * installed them on this host (CI) or in the podman machine (make
 * prod-local). Chromium resolves *.test to RONOTE_SMOKE_HOST, and trusts
 * Caddy's own CA by its key, so the pages are secure contexts and can be
 * cross-origin isolated.
 */

const host = process.env.RONOTE_SMOKE_HOST ?? "127.0.0.1";
const ca = process.env.RONOTE_SMOKE_CA;
if (!ca) {
  throw new Error("RONOTE_SMOKE_CA is not set: run the smoke suite with make prod-smoke");
}
const spki = createHash("sha256")
  .update(createPublicKey(readFileSync(ca)).export({ type: "spki", format: "der" }))
  .digest("base64");

export default defineConfig({
  testDir: "smoke",
  forbidOnly: Boolean(process.env.CI),
  timeout: 90_000,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "line",
  use: {
    baseURL: "https://ronote.test",
    trace: "retain-on-failure",
    launchOptions: {
      args: [
        `--host-resolver-rules=MAP *.test ${host}`,
        `--ignore-certificate-errors-spki-list=${spki}`,
      ],
    },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});

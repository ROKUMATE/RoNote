import { defineConfig } from "vitest/config";

// Tests that need the network, run only by make test-live, never in CI.
export default defineConfig({
  test: { environment: "node", include: ["live/**/*.test.ts"], testTimeout: 120_000 },
});

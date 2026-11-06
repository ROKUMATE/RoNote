import { expect, test, vi } from "vitest";

// make web-dev's two servers, as their configs set them up with nothing in
// the environment: each must name the other, or code never runs in dev.
test("the runner talks to the app at the address the app is served on", async () => {
  vi.stubEnv("RONOTE_APP_ORIGINS", undefined);
  vi.stubEnv("RONOTE_RUNNER_ORIGIN", undefined);
  const { default: app } = await import("./vite.config.ts");
  const { default: runner, headersFor } = await import("./vite.runner.config.ts");

  // An address, not localhost, which may resolve to IPv6 alone.
  const appOrigin = `http://${app.server?.host}:${app.server?.port}`;
  expect(appOrigin).toBe("http://127.0.0.1:5173");
  expect(app.server?.strictPort, "a busy port must not move the app onto the runner's").toBe(true);
  expect(runner.define?.["import.meta.env.RONOTE_APP_ORIGINS"]).toBe(JSON.stringify([appOrigin]));
  expect(headersFor("/")["Content-Security-Policy"]).toContain(`frame-ancestors ${appOrigin};`);
  expect(JSON.parse(app.define?.["import.meta.env.RONOTE_RUNNER_ORIGIN"])).toBe(
    `http://localhost:${runner.server?.port}`,
  );
});

// The JavaScript worker evaluates the reader's code, which its policy alone
// allows; the runner page and Python keep theirs, and nothing reaches out.
test("only the JavaScript worker's script may evaluate code", async () => {
  const { headersFor } = await import("./vite.runner.config.ts");
  const policy = (path: string) => headersFor(path)["Content-Security-Policy"] ?? "";
  for (const path of ["/javascript.worker.ts", "/assets/javascript.worker-Bq3x9Z.js"]) {
    expect(policy(path), path).toContain("script-src 'self' 'unsafe-eval';");
    expect(policy(path), path).toContain("connect-src 'self';");
  }
  for (const path of [
    "/",
    "/main.ts",
    "/python.worker.ts",
    "/pyodide/pyodide.asm.mjs",
    "/evaluate.ts",
  ]) {
    expect(policy(path), path).toContain("script-src 'self' 'wasm-unsafe-eval';");
    expect(policy(path), path).not.toContain("'unsafe-eval'");
  }
});

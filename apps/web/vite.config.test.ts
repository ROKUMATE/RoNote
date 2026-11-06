import { expect, test, vi } from "vitest";

// make web-dev's two servers, as their configs set them up with nothing in
// the environment: each must name the other, or code never runs in dev.
test("the runner talks to the app at the address the app is served on", async () => {
  vi.stubEnv("RONOTE_APP_ORIGINS", undefined);
  vi.stubEnv("RONOTE_RUNNER_ORIGIN", undefined);
  const { default: app } = await import("./vite.config.ts");
  const { default: runner } = await import("./vite.runner.config.ts");

  // An address, not localhost, which may resolve to IPv6 alone.
  const appOrigin = `http://${app.server?.host}:${app.server?.port}`;
  expect(appOrigin).toBe("http://127.0.0.1:5173");
  expect(app.server?.strictPort, "a busy port must not move the app onto the runner's").toBe(true);
  expect(runner.define?.["import.meta.env.RONOTE_APP_ORIGINS"]).toBe(JSON.stringify([appOrigin]));
  expect(runner.server?.headers?.["Content-Security-Policy"]).toContain(
    `frame-ancestors ${appOrigin};`,
  );
  expect(JSON.parse(app.define?.["import.meta.env.RONOTE_RUNNER_ORIGIN"])).toBe(
    `http://localhost:${runner.server?.port}`,
  );
});

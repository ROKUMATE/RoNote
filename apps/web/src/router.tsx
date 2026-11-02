import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  type RouterHistory,
} from "@tanstack/react-router";
import { Library } from "./pages/Library";
import { NotFound } from "./pages/NotFound";
import { AppShell } from "./shell/AppShell";

const rootRoute = createRootRoute({ component: AppShell, notFoundComponent: NotFound });

const libraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Library,
});

const routeTree = rootRoute.addChildren([libraryRoute]);

export function createAppRouter(history: RouterHistory = createBrowserHistory()) {
  return createRouter({ routeTree, history });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}

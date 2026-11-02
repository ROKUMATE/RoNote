import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
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

/**
 * Token and typography reference for development. Production builds replace
 * import.meta.env.DEV with false, so this route and its chunk are dropped.
 */
function styleguideRoute() {
  return createRoute({
    getParentRoute: () => rootRoute,
    path: "/_styleguide",
    component: lazyRouteComponent(() => import("./styleguide/Styleguide"), "Styleguide"),
  });
}

const routeTree = rootRoute.addChildren([
  libraryRoute,
  ...(import.meta.env.DEV ? [styleguideRoute()] : []),
]);

export function createAppRouter(history: RouterHistory = createBrowserHistory()) {
  return createRouter({ routeTree, history });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}

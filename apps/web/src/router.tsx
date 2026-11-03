import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  type RouterHistory,
} from "@tanstack/react-router";
import { DocumentPage } from "./pages/DocumentPage";
import { Library } from "./pages/Library";
import { NotFound } from "./pages/NotFound";
import { AppShell } from "./shell/AppShell";

const rootRoute = createRootRoute({ component: AppShell, notFoundComponent: NotFound });

const libraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Library,
});

const documentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/doc/$id",
  component: DocumentPage,
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
  documentRoute,
  ...(import.meta.env.DEV ? [styleguideRoute()] : []),
]);

export function createAppRouter(history: RouterHistory = createBrowserHistory()) {
  return createRouter({ routeTree, history });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
  interface HistoryState {
    /** Set when an import found a page already in the library. */
    notice?: "existing";
  }
}

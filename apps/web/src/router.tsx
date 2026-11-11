import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  type RouterHistory,
} from "@tanstack/react-router";
import { Library } from "./pages/Library";
import { Login, validateLoginSearch } from "./pages/Login";
import { NotFound } from "./pages/NotFound";
import { AppShell } from "./shell/AppShell";

const rootRoute = createRootRoute({
  notFoundComponent: () => (
    <AppShell>
      <NotFound />
    </AppShell>
  ),
});

/** Every page but sign-in sits in the app's frame. */
const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: AppShell,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: validateLoginSearch,
  component: function LoginPage() {
    return <Login search={loginRoute.useSearch()} />;
  },
});

const libraryRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: Library,
});

/** The document reader and editor, with everything it needs, is its own chunk. */
const documentRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/doc/$id",
  component: lazyRouteComponent(() => import("./pages/DocumentPage"), "DocumentPage"),
});

/**
 * Token and typography reference for development. Production builds replace
 * import.meta.env.DEV with false, so this route and its chunk are dropped.
 */
function styleguideRoute() {
  return createRoute({
    getParentRoute: () => shellRoute,
    path: "/_styleguide",
    component: lazyRouteComponent(() => import("./styleguide/Styleguide"), "Styleguide"),
  });
}

const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([
    libraryRoute,
    documentRoute,
    ...(import.meta.env.DEV ? [styleguideRoute()] : []),
  ]),
]);

export function createAppRouter(history: RouterHistory = createBrowserHistory()) {
  // Back and forward return to where the reader was on each page.
  return createRouter({ routeTree, history, scrollRestoration: true });
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

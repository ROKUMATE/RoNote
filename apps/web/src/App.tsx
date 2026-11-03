import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { ImportsProvider, type ImportTracker } from "./imports/tracker";
import type { createAppRouter } from "./router";

/** The app: server state, the imports this session follows, and the routes. */
export function App({
  router,
  queryClient,
  tracker,
}: {
  router: ReturnType<typeof createAppRouter>;
  queryClient: QueryClient;
  tracker: ImportTracker;
}) {
  return (
    <QueryClientProvider client={queryClient}>
      <ImportsProvider tracker={tracker}>
        <RouterProvider router={router} />
      </ImportsProvider>
    </QueryClientProvider>
  );
}

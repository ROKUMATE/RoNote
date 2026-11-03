import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { App } from "../App";
import { createQueryClient } from "../api/queries";
import { ImportTracker } from "../imports/tracker";
import { createAppRouter } from "../router";

/** Renders the whole app at path, with nothing retried. */
export function renderApp(path = "/") {
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({
    queries: { ...queryClient.getDefaultOptions().queries, retry: false },
  });
  const tracker = new ImportTracker(queryClient);
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }));
  const result = render(<App router={router} queryClient={queryClient} tracker={tracker} />);
  return { ...result, router, queryClient, tracker };
}

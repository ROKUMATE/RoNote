import { useQuery } from "@tanstack/react-query";
import { useParams, useRouterState } from "@tanstack/react-router";
import { documentQuery } from "../api/queries";
import { PageTitle } from "../ui/PageTitle";

export function DocumentPage() {
  const { id } = useParams({ from: "/doc/$id" });
  const doc = useQuery(documentQuery(id));
  const existing = useRouterState({ select: (s) => s.location.state.notice === "existing" });

  return (
    <article className="max-w-[68ch]">
      {existing && (
        <p role="status" className="mb-4 text-ink-2 text-ui-lg">
          Already in your library
        </p>
      )}
      {doc.data && <PageTitle>{doc.data.title}</PageTitle>}
    </article>
  );
}

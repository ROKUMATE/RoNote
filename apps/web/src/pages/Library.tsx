import { ImportButton } from "../imports/ImportDialog";
import { PageTitle } from "../ui/PageTitle";

export function Library() {
  return (
    <div className="max-w-[68ch]">
      <PageTitle>Library</PageTitle>
      <p className="mt-3 text-ink-2 text-ui-lg">
        No pages yet. Import a documentation page or a printed PDF to start.
      </p>
      <div className="mt-6">
        <ImportButton />
      </div>
    </div>
  );
}

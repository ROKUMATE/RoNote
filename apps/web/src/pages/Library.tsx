import { FilePlusIcon } from "@phosphor-icons/react/FilePlus";
import { PageTitle } from "../ui/PageTitle";
import { Tooltip } from "../ui/Tooltip";

const importHint = import.meta.env.DEV ? "Coming in P08" : "Not available yet";

export function Library() {
  return (
    <div className="max-w-[68ch]">
      <PageTitle>Library</PageTitle>
      <p className="mt-3 text-ink-2 text-ui-lg">
        No pages yet. Import a documentation page or a printed PDF to start.
      </p>
      <div className="mt-6">
        <Tooltip label={importHint}>
          {(trigger) => (
            <button
              type="button"
              aria-disabled="true"
              {...trigger}
              className="inline-flex cursor-not-allowed items-center gap-2 rounded-sm border border-rule px-3 py-1.5 text-ink-2 text-ui-lg"
            >
              <FilePlusIcon size={16} aria-hidden />
              Import page
            </button>
          )}
        </Tooltip>
      </div>
    </div>
  );
}

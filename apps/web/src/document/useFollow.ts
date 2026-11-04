import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { requestErrorMessage } from "../api/messages";
import { createImport } from "../api/queries";
import { endingMessage, stepLabels } from "../imports/describe";
import { useImportTracker, useTrackedImports } from "../imports/tracker";
import { type LinkTargets, linkStatus, splitFragment } from "./links";
import type { Following } from "./Reader";

type Started = { href: string; link: HTMLElement; importId?: string; failure?: string };

/**
 * Following same-domain links from a document: a page in the library opens
 * at once, at the link's #fragment; any other is imported first, its steps
 * shown next to the link, and opens when done.
 */
export function useFollow(targets: LinkTargets | undefined) {
  const navigate = useNavigate();
  const tracker = useImportTracker();
  const imports = useTrackedImports();
  const [started, setStarted] = useState<Started>();

  const open = (documentId: string, href: string) => {
    const { fragment } = splitFragment(href);
    setStarted(undefined);
    void navigate({
      to: "/doc/$id",
      params: { id: documentId },
      ...(fragment ? { hash: fragment } : {}),
    });
  };

  const follow = async (href: string, link: HTMLElement) => {
    const status = linkStatus(href, targets);
    if (status.state === "internal") {
      open(status.documentId, href);
      return;
    }
    setStarted({ href, link });
    try {
      const created = await createImport(splitFragment(href).url);
      if (created.existing) {
        open(created.document.id, href);
        return;
      }
      tracker.track(created.import);
      setStarted({ href, link, importId: created.import.id });
    } catch (error) {
      setStarted({ href, link, failure: requestErrorMessage(error) });
    }
  };

  const tracked = started?.importId ? imports.find((i) => i.id === started.importId) : undefined;
  useEffect(() => {
    if (started && tracked?.status === "done" && tracked.documentId)
      open(tracked.documentId, started.href);
  });

  let following: Following | undefined;
  if (started) {
    const failure = started.failure ?? (tracked && endingMessage(tracked));
    const step =
      tracked && tracked.status !== "failed" ? stepLabels[tracked.status] : stepLabels.queued;
    following = {
      link: started.link,
      href: started.href,
      failed: Boolean(failure),
      text: failure ?? step,
    };
  }
  return { follow, following };
}

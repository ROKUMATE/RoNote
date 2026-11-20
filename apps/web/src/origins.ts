/*
 * The app's and the runner's origins, read at run time from the page's own
 * meta tags (docs/adr/0014-production-topology.md). Caddy fills them in
 * production and Vite in dev, so one build serves any deployment's domains.
 */

type Name = "ronote-app-url" | "ronote-runner-url";

/** The origin a meta tag names, or "" when it names none. */
export function configuredOrigin(name: Name, doc: Document = document): string {
  const content = doc.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content ?? "";
  try {
    const url = new URL(content);
    return url.protocol === "https:" || url.protocol === "http:" ? url.origin : "";
  } catch {
    return "";
  }
}

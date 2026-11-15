/** The host of a URL, port included, or the URL itself if it does not parse. */
export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/** Where a document came from, in a word: its PDF's filename, or its page's host. */
export function sourceOf(doc: { source_url: string; pdf?: { filename: string } }): string {
  return doc.pdf ? doc.pdf.filename : hostOf(doc.source_url);
}

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

/** A date for people, in their locale: "3 Nov 2026". */
export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso));
}

/** The host of a URL, port included, or the URL itself if it does not parse. */
export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

/** A date for people, in their locale: "3 Nov 2026". */
export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso));
}

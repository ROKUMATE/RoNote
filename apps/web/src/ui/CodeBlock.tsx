/** A code block that keeps its line breaks and scrolls sideways when too wide. */
export function CodeBlock({ code }: { code: string }) {
  return (
    // biome-ignore lint/a11y/noNoninteractiveTabindex: content that scrolls must be reachable by keyboard (WCAG 2.1.1; axe scrollable-region-focusable).
    <pre tabIndex={0}>
      <code>{code}</code>
    </pre>
  );
}

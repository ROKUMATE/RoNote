import { CopyIcon } from "@phosphor-icons/react/Copy";
import { type ReactNode, type Ref, useState } from "react";

type CodeBlockProps = {
  code: string;
  /** The code's language, as a label or a control to choose it. */
  language?: ReactNode;
  title?: string | undefined;
  /** Where the title links to, when the page linked it to the file. */
  titleHref?: string | undefined;
  /** The code drawn another way, such as highlighted; code is still what Copy copies. */
  children?: ReactNode;
  ref?: Ref<HTMLDivElement>;
};

/**
 * A code block that keeps its line breaks and scrolls sideways when too
 * wide, with its title, language and a Copy button when it has a header.
 */
export function CodeBlock({ code, language, title, titleHref, children, ref }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
  };
  return (
    <div ref={ref} className="code-block">
      <div className="code-header">
        {title &&
          (titleHref ? (
            <a href={titleHref} target="_blank" rel="noopener noreferrer" className="code-title">
              {title}
            </a>
          ) : (
            <span className="code-title">{title}</span>
          ))}
        {language}
        <button
          type="button"
          onClick={() => void copy()}
          onBlur={() => setCopied(false)}
          aria-label={copied ? "Copied code" : "Copy code"}
          className="code-copy"
        >
          <CopyIcon size={16} aria-hidden />
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: content that scrolls must be reachable by keyboard (WCAG 2.1.1; axe scrollable-region-focusable). */}
      <pre tabIndex={0}>
        <code>{children ?? code}</code>
      </pre>
    </div>
  );
}

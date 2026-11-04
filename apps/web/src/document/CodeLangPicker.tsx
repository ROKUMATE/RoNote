import { CaretDownIcon } from "@phosphor-icons/react/CaretDown";
import { type FocusEvent, type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import type { CodeLang, IrBlock } from "../api/schema";
import { type CodeLangChoices, codeLangLabels, codeLangs } from "./codeLangs";

/** The picker's options: first the page's own language (undefined), then RoNote's. */
const options: readonly (CodeLang | undefined)[] = [undefined, ...codeLangs];

/**
 * A code block's language, as a button opening a listbox to choose
 * another. The page's language is the default; a choice of the user's is
 * shown as theirs. Arrow keys, Home and End move, a letter jumps to the
 * next language starting with it, Enter chooses, Escape closes.
 */
export function CodeLangPicker({ block, choices }: { block: IrBlock; choices: CodeLangChoices }) {
  const chosen = choices.chosen[block.id];
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [forAll, setForAll] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const id = useId();
  const page = block.lang || "plain text";
  const label = (lang: CodeLang | undefined) => (lang ? codeLangLabels[lang] : `${page} (page)`);

  useEffect(() => {
    if (open) list.current?.focus();
  }, [open]);
  useEffect(() => {
    if (open) document.getElementById(`${id}-${active}`)?.scrollIntoView({ block: "nearest" });
  }, [open, active, id]);

  const show = () => {
    setActive(options.indexOf(chosen));
    setOpen(true);
  };
  const close = () => {
    setOpen(false);
    setForAll(false);
  };
  const pick = (lang: CodeLang | undefined) => {
    choices.choose(block.id, lang);
    if (lang && forAll) choices.chooseForUnlabelled(lang);
    close();
    button.current?.focus();
  };

  const move = (event: KeyboardEvent) => {
    const last = options.length - 1;
    const to: Record<string, number> = {
      ArrowDown: Math.min(active + 1, last),
      ArrowUp: Math.max(active - 1, 0),
      Home: 0,
      End: last,
    };
    if (event.key in to) {
      setActive(to[event.key] ?? active);
    } else if (event.key === "Enter" || event.key === " ") {
      pick(options[active]);
    } else if (event.key.length === 1 && /\S/.test(event.key)) {
      const key = event.key.toLowerCase();
      const next = options
        .map((_, i) => (active + 1 + i) % options.length)
        .find((i) => label(options[i]).toLowerCase().startsWith(key));
      if (next === undefined) return;
      setActive(next);
    } else {
      return;
    }
    event.preventDefault();
  };

  const leave = (event: FocusEvent) => {
    const to = event.relatedTarget;
    if (!event.currentTarget.contains(to) && to !== button.current) close();
  };

  return (
    <span className="code-lang-picker">
      <button
        ref={button}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? `${id}-list` : undefined}
        onClick={() => (open ? close() : show())}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            show();
          }
        }}
        className="code-lang"
      >
        <span className="sr-only">Language: </span>
        {chosen ? (
          <>
            {codeLangLabels[chosen]}
            <span className="code-lang-yours"> · yours</span>
          </>
        ) : (
          page
        )}
        <CaretDownIcon size={16} aria-hidden />
      </button>
      {open && (
        // biome-ignore lint/a11y/noStaticElementInteractions: Escape closes the popup from the listbox or the checkbox, each focusable itself.
        <div
          className="code-lang-popup"
          onBlur={leave}
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            close();
            button.current?.focus();
          }}
        >
          <div
            ref={list}
            id={`${id}-list`}
            role="listbox"
            aria-label="Code language"
            tabIndex={-1}
            aria-activedescendant={`${id}-${active}`}
            onKeyDown={move}
          >
            {options.map((lang, i) => (
              // biome-ignore lint/a11y/useKeyWithClickEvents: the listbox takes the keys for its options, through aria-activedescendant.
              <div
                key={lang ?? "page"}
                id={`${id}-${i}`}
                role="option"
                tabIndex={-1}
                aria-selected={lang === chosen}
                data-active={i === active || undefined}
                onClick={() => pick(lang)}
                onMouseMove={() => setActive(i)}
              >
                {label(lang)}
              </div>
            ))}
          </div>
          <label className="code-lang-all">
            <input
              type="checkbox"
              checked={forAll}
              onChange={(event) => setForAll(event.target.checked)}
            />
            Use for all unlabelled code in this document
          </label>
        </div>
      )}
    </span>
  );
}

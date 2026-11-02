import { GearSixIcon } from "@phosphor-icons/react/GearSix";
import { useId, useState } from "react";
import {
  applyReaderFont,
  applyTheme,
  type ReaderFont,
  readReaderFont,
  readTheme,
  saveReaderFont,
  saveTheme,
  type Theme,
} from "../preferences";
import { ChoiceGroup } from "../ui/ChoiceGroup";

const themeOptions = [
  ["system", "System"],
  ["light", "Light"],
  ["dark", "Dark"],
] as const satisfies readonly (readonly [Theme, string])[];

const readerFontOptions = [
  ["serif", "Serif"],
  ["sans", "Sans"],
] as const satisfies readonly (readonly [ReaderFont, string])[];

/**
 * The settings button and its popover. The native popover API provides
 * light dismiss, Escape and focus order.
 */
export function Settings() {
  const popoverId = useId();
  const [theme, setTheme] = useState(readTheme);
  const [readerFont, setReaderFont] = useState(readReaderFont);

  const chooseTheme = (next: Theme) => {
    setTheme(next);
    saveTheme(next);
    applyTheme(next);
  };
  const chooseReaderFont = (next: ReaderFont) => {
    setReaderFont(next);
    saveReaderFont(next);
    applyReaderFont(next);
  };

  return (
    <>
      <button
        type="button"
        popoverTarget={popoverId}
        className="flex items-center gap-2 rounded-sm px-2 py-1 text-ink-2 text-ui-lg hover:bg-paper-2 hover:text-ink"
      >
        <GearSixIcon size={16} aria-hidden />
        Settings
      </button>
      <div
        id={popoverId}
        popover="auto"
        role="dialog"
        aria-label="Settings"
        className="fixed inset-auto top-12 right-2 m-0 w-60 space-y-4 rounded-md border border-rule bg-paper p-4 text-ink opacity-0 open:opacity-100 sidebar:top-auto sidebar:right-auto sidebar:bottom-16 sidebar:left-4 motion-safe:transition-[opacity,display,overlay] motion-safe:transition-discrete starting:open:opacity-0"
      >
        <ChoiceGroup
          legend="Theme"
          name="theme"
          options={themeOptions}
          value={theme}
          onChange={chooseTheme}
        />
        <ChoiceGroup
          legend="Reader font"
          name="reader-font"
          options={readerFontOptions}
          value={readerFont}
          onChange={chooseReaderFont}
        />
      </div>
    </>
  );
}

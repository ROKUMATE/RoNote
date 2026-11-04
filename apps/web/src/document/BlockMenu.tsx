import type { Icon } from "@phosphor-icons/react";
import { InfoIcon } from "@phosphor-icons/react/Info";
import { ListBulletsIcon } from "@phosphor-icons/react/ListBullets";
import { MinusIcon } from "@phosphor-icons/react/Minus";
import { QuotesIcon } from "@phosphor-icons/react/Quotes";
import { TextHTwoIcon } from "@phosphor-icons/react/TextHTwo";
import { TextTIcon } from "@phosphor-icons/react/TextT";
import { type KeyboardEvent, useEffect, useRef } from "react";
import type { UserBlockKind } from "../api/schema";
import { userBlockKinds, userBlockLabels } from "./userBlocks";

const icons: Record<UserBlockKind, Icon> = {
  paragraph: TextTIcon,
  heading: TextHTwoIcon,
  list: ListBulletsIcon,
  quote: QuotesIcon,
  callout: InfoIcon,
  divider: MinusIcon,
};

/**
 * The menu of blocks a user can add: exactly the six user kinds. Arrow keys
 * move, Enter chooses, Escape and leaving the menu close it.
 */
export function BlockMenu({
  top,
  onChoose,
  onClose,
}: {
  top: number;
  onChoose: (kind: UserBlockKind) => void;
  onClose: () => void;
}) {
  const menu = useRef<HTMLDivElement>(null);

  useEffect(() => {
    menu.current?.querySelector("button")?.focus();
  }, []);

  const move = (event: KeyboardEvent) => {
    const items = [...(menu.current?.querySelectorAll("button") ?? [])];
    const at = items.indexOf(document.activeElement as HTMLButtonElement);
    const step = { ArrowDown: 1, ArrowUp: -1 }[event.key];
    if (step) {
      event.preventDefault();
      items[(at + step + items.length) % items.length]?.focus();
    } else if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    }
  };

  return (
    <div
      ref={menu}
      role="menu"
      aria-label="Add a block"
      tabIndex={-1}
      onKeyDown={move}
      onBlur={(e) => {
        if (!menu.current?.contains(e.relatedTarget)) onClose();
      }}
      style={{ top }}
      className="absolute left-0 z-10 w-48 rounded-md border border-rule bg-paper py-1 font-sans text-ink text-ui-lg"
    >
      {userBlockKinds.map((kind) => {
        const KindIcon = icons[kind];
        return (
          <button
            key={kind}
            type="button"
            role="menuitem"
            data-kind={kind}
            onClick={() => onChoose(kind)}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-paper-2 focus-visible:bg-paper-2"
          >
            <KindIcon size={16} aria-hidden />
            {userBlockLabels[kind]}
          </button>
        );
      })}
    </div>
  );
}

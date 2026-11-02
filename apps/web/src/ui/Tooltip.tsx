import {
  type FocusEventHandler,
  type PointerEventHandler,
  type ReactNode,
  useEffect,
  useId,
  useState,
} from "react";

type TriggerProps = {
  "aria-describedby": string;
  onFocus: FocusEventHandler;
  onPointerEnter: PointerEventHandler;
};

type TooltipProps = {
  label: string;
  children: (trigger: TriggerProps) => ReactNode;
};

/**
 * Shows label under its trigger while the trigger is hovered or focused, and
 * while the label itself is hovered. Escape hides it until the trigger is
 * hovered or focused again (WCAG 1.4.13). The trigger is described by the
 * label even while it is hidden.
 */
export function Tooltip({ label, children }: TooltipProps) {
  const id = useId();
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setDismissed(true);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const reshow = () => setDismissed(false);

  return (
    <span className="group relative inline-flex">
      {children({ "aria-describedby": id, onFocus: reshow, onPointerEnter: reshow })}
      <span
        id={id}
        role="tooltip"
        hidden={dismissed}
        className="invisible absolute top-full left-0 pt-2 opacity-0 group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100 motion-safe:transition-[opacity,visibility]"
      >
        <span className="block whitespace-nowrap rounded-sm border border-rule bg-paper-2 px-2 py-1 text-ink text-ui">
          {label}
        </span>
      </span>
    </span>
  );
}

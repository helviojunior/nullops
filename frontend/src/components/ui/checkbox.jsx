import { Check, Minus } from "lucide-react";
import { cn } from "lib/utils";

// Row-selection checkbox (table multi-select). Styled control instead of the
// native <input type="checkbox">; boolean *settings* use the Switch.
export function Checkbox({ checked, indeterminate, onChange, ariaLabel, className }) {
  const on = checked || indeterminate;
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={indeterminate ? "mixed" : !!checked}
      aria-label={ariaLabel}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!checked);
      }}
      className={cn(
        "inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        on ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/40 bg-transparent",
        className
      )}
    >
      {indeterminate ? <Minus className="h-3 w-3" /> : checked ? <Check className="h-3 w-3" /> : null}
    </button>
  );
}

import React from "react";

// iOS-style toggle switch — the standard boolean control across the app.
// Use this everywhere instead of a native <input type="checkbox">.
export function Switch({ checked, onChange, disabled = false, id, ariaLabel }) {
  return (
    <button
      type="button"
      role="switch"
      id={id}
      aria-checked={!!checked}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={() => !disabled && onChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:opacity-50 disabled:cursor-not-allowed ${
        checked ? "bg-primary" : "bg-muted-foreground/30"
      }`}
    >
      <span
        className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${
          checked ? "translate-x-[22px]" : "translate-x-0.5"
        }`}
      />
    </button>
  );
}

/**
 * Standard boolean row: label (with optional description) on the left and the
 * Switch on the right. For a bare toggle compose `flex items-center gap-3`
 * with <Switch /> directly.
 */
export function SwitchField({ checked, onChange, label, description, disabled }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-border p-4">
      <div className="space-y-0.5">
        <p className="text-sm font-medium leading-none">{label}</p>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch
        checked={checked}
        onChange={onChange}
        disabled={disabled}
        ariaLabel={typeof label === "string" ? label : undefined}
      />
    </div>
  );
}

export default Switch;
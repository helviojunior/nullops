import React, { useEffect } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { useI18n } from "i18n";
import { cn } from "lib/utils";

const sizeStyles = {
  sm: "max-w-sm",
  md: "max-w-md",
  lg: "max-w-lg",
  xl: "max-w-2xl",
};

/**
 * Base modal of the system.
 *
 * Used for confirmations, short messages and small forms — never to show
 * the detail of an object (details always get their own route).
 */
function Modal({
  open,
  onClose,
  title,
  description,
  icon,
  size = "sm",
  showClose = true,
  footer,
  children,
  className,
}) {
  const { t } = useI18n();

  useEffect(() => {
    if (!open) return undefined;

    const handleKeyDown = (e) => {
      if (e.key === "Escape" && onClose) onClose();
    };

    document.addEventListener("keydown", handleKeyDown);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.body.style.overflow = previousOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/40 p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && onClose) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === "string" ? title : undefined}
        className={cn(
          "relative w-full bg-card rounded-2xl shadow-xl border border-border animate-fade-in",
          sizeStyles[size] || sizeStyles.sm,
          className
        )}
      >
        {showClose && onClose && (
          <button
            type="button"
            onClick={onClose}
            className="absolute right-3 top-3 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
            aria-label={t("common.close")}
          >
            <X className="h-4 w-4" />
          </button>
        )}

        <div className="p-6 text-center">
          {icon && <div className="mb-4 flex justify-center">{icon}</div>}
          {title && <h2 className="text-lg font-semibold mb-2">{title}</h2>}
          {description && (
            <div className="text-sm text-muted-foreground">{description}</div>
          )}
          {children && <div className="text-sm mt-2">{children}</div>}
          {footer && (
            <div className="flex justify-center gap-3 mt-6">{footer}</div>
          )}
        </div>
      </div>
    </div>,
    document.body
  );
}

export { Modal };

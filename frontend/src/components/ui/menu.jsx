import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { MoreHorizontal } from "lucide-react";
import { cn } from "lib/utils";

/**
 * Small action menu ("⋯"). Rendered in a portal so table scrolling does not
 * clip it; closes on outside click, Escape or after an item is chosen.
 * items: [{ label, icon: Icon, onClick, danger, hidden }]
 */
export function ActionMenu({ items, title }) {
  const [pos, setPos] = useState(null);
  const btn = useRef(null);
  const panel = useRef(null);
  const visible = items.filter((i) => !i.hidden);

  useEffect(() => {
    if (!pos) return undefined;
    const close = (e) => {
      if (panel.current?.contains(e.target) || btn.current?.contains(e.target)) return;
      setPos(null);
    };
    const esc = (e) => e.key === "Escape" && setPos(null);
    window.addEventListener("mousedown", close);
    window.addEventListener("keydown", esc);
    window.addEventListener("resize", () => setPos(null), { once: true });
    return () => {
      window.removeEventListener("mousedown", close);
      window.removeEventListener("keydown", esc);
    };
  }, [pos]);

  if (!visible.length) return null;

  const toggle = (e) => {
    e.stopPropagation();
    if (pos) return setPos(null);
    const r = btn.current.getBoundingClientRect();
    const height = visible.length * 34 + 8;
    const top = r.bottom + height > window.innerHeight ? r.top - height - 4 : r.bottom + 4;
    setPos({ top, right: window.innerWidth - r.right });
  };

  return (
    <>
      <button
        ref={btn}
        type="button"
        title={title}
        aria-label={title}
        aria-haspopup="menu"
        aria-expanded={!!pos}
        onClick={toggle}
        className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        <MoreHorizontal className="h-4 w-4" />
      </button>
      {pos &&
        createPortal(
          <div
            ref={panel}
            role="menu"
            className="fixed z-[90] min-w-44 rounded-lg border border-border bg-card p-1 shadow-xl animate-fade-in"
            style={{ top: pos.top, right: pos.right }}
            onClick={(e) => e.stopPropagation()}
          >
            {visible.map(({ label, icon: Icon, onClick, danger }) => (
              <button
                key={label}
                type="button"
                role="menuitem"
                onClick={() => {
                  setPos(null);
                  onClick();
                }}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm hover:bg-muted",
                  danger && "text-red-500"
                )}
              >
                {Icon && <Icon className="h-4 w-4" />}
                {label}
              </button>
            ))}
          </div>,
          document.body
        )}
    </>
  );
}

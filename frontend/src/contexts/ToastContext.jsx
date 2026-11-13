import { createContext, useCallback, useContext, useRef, useState } from "react";
import { CheckCircle2, AlertTriangle, X } from "lucide-react";
import { cn } from "lib/utils";

// Non-blocking notifications for completed actions (never native alerts).
const ToastContext = createContext(null);

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);
  const seq = useRef(0);

  const dismiss = useCallback((id) => setToasts((all) => all.filter((x) => x.id !== id)), []);

  const push = useCallback(
    (message, variant = "success") => {
      const id = ++seq.current;
      setToasts((all) => [...all.slice(-4), { id, message, variant }]);
      setTimeout(() => dismiss(id), variant === "error" ? 6000 : 3500);
    },
    [dismiss]
  );

  const api = { success: (m) => push(m, "success"), error: (m) => push(m, "error") };

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="fixed bottom-4 right-4 z-[120] flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            role="status"
            className={cn(
              "flex items-start gap-2 rounded-lg border bg-card p-3 text-sm shadow-lg animate-fade-in",
              t.variant === "error" ? "border-red-500/40" : "border-emerald-500/40"
            )}
          >
            {t.variant === "error" ? (
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-500" />
            ) : (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-500" />
            )}
            <span className="flex-1 break-words">{t.message}</span>
            <button onClick={() => dismiss(t.id)} className="text-muted-foreground hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within <ToastProvider>");
  return ctx;
}

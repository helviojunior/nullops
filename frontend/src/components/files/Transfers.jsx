import { X, ArrowUp, ArrowDown, CheckCircle2, AlertTriangle } from "lucide-react";
import { useI18n } from "i18n";
import { humanSize } from "lib/utils";

// Upload / download progress panel.
export default function Transfers({ items, onClear }) {
  const { t } = useI18n();
  if (!items.length) return null;
  return (
    <div className="fixed bottom-4 left-4 z-[110] w-80 max-w-[calc(100vw-2rem)] rounded-lg border border-border bg-card shadow-lg">
      <div className="flex items-center justify-between border-b border-border px-3 py-2 text-sm font-medium">
        {t("upload.title")}
        <button onClick={onClear} className="text-muted-foreground hover:text-foreground" aria-label={t("common.close")}>
          <X className="h-4 w-4" />
        </button>
      </div>
      <ul className="max-h-64 overflow-auto scrollbar-thin">
        {items.map((it) => {
          const pct = it.total ? Math.min(100, Math.round((it.done / it.total) * 100)) : it.state === "done" ? 100 : 0;
          return (
            <li key={it.id} className="px-3 py-2 text-xs">
              <div className="flex items-center gap-2">
                {it.kind === "upload" ? (
                  <ArrowUp className="h-3.5 w-3.5 text-primary" />
                ) : (
                  <ArrowDown className="h-3.5 w-3.5 text-primary" />
                )}
                <span className="flex-1 truncate" title={it.name}>
                  {it.name}
                </span>
                {it.state === "done" && <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />}
                {it.state === "error" && <AlertTriangle className="h-3.5 w-3.5 text-red-500" />}
                <span className="text-muted-foreground">
                  {it.state === "error" ? t("upload.failed") : it.state === "done" ? t("upload.done") : `${pct}%`}
                </span>
              </div>
              <div className="mt-1 h-1 overflow-hidden rounded bg-muted">
                <div
                  className={`h-full transition-all ${it.state === "error" ? "bg-red-500" : "bg-primary"}`}
                  style={{ width: `${it.state === "error" ? 100 : pct}%` }}
                />
              </div>
              {it.error ? (
                <div className="mt-1 text-red-500">{it.error}</div>
              ) : (
                <div className="mt-1 text-muted-foreground">
                  {humanSize(it.done)} / {humanSize(it.total)}
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

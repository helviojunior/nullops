import { useNavigate } from "react-router-dom";
import { AlertTriangle, ArrowDown, ArrowLeft, ArrowUp, Ban, CheckCircle2, Loader2, Trash2, X } from "lucide-react";
import { useTransfers } from "contexts/TransferContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { cn, humanSize } from "lib/utils";

function eta(item) {
  if (!item.speed || !item.total || item.done >= item.total) return null;
  const s = Math.round((item.total - item.done) / item.speed);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  return h ? `${h}h ${String(m).padStart(2, "0")}m` : `${m}:${String(sec).padStart(2, "0")}`;
}

/** Downloads and uploads of this browser session (own route: /transfers). */
export default function Transfers() {
  const { items, summary, cancel, cancelAll, clearFinished } = useTransfers();
  const navigate = useNavigate();
  const { t, tf } = useI18n();
  const finished = items.filter((x) => x.state !== "running").length;

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" /> {t("common.back")}
        </Button>
        <div className="min-w-0 flex-1">
          <div className="font-medium">{t("transfers.title")}</div>
          <div className="text-xs text-muted-foreground">
            {summary.running
              ? tf("transfers.summaryRunning", {
                  n: summary.running,
                  pct: summary.progress === null ? "…" : `${Math.floor(summary.progress * 100)}%`,
                  speed: humanSize(summary.speed),
                })
              : t("transfers.summaryIdle")}
          </div>
        </div>
        {summary.running > 0 && (
          <Button variant="outline" size="sm" onClick={cancelAll}>
            <Ban className="h-4 w-4" /> {t("transfers.cancelAll")}
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={clearFinished} disabled={!finished}>
          <Trash2 className="h-4 w-4" /> {t("transfers.clear")}
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-4 scrollbar-thin">
        {items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 p-16 text-sm text-muted-foreground">
            <ArrowDown className="h-8 w-8" />
            {t("transfers.empty")}
          </div>
        ) : (
          <ul className="space-y-2">
            {items.map((it) => {
              const pct = it.total ? Math.min(100, (it.done / it.total) * 100) : it.state === "done" ? 100 : null;
              const KindIcon = it.kind === "upload" ? ArrowUp : ArrowDown;
              return (
                <li key={it.id} className="rounded-lg border border-border bg-card p-3">
                  <div className="flex items-center gap-3">
                    <KindIcon className="h-4 w-4 shrink-0 text-primary" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium" title={it.name}>
                        {it.name}
                      </div>
                      <div className="truncate text-xs text-muted-foreground" title={it.path}>
                        {t(it.kind === "upload" ? "transfers.upload" : "transfers.download")} · /{it.path}
                      </div>
                    </div>
                    <span
                      className={cn(
                        "inline-flex items-center gap-1 text-xs",
                        it.state === "done" && "text-emerald-500",
                        it.state === "error" && "text-red-500",
                        it.state === "canceled" && "text-muted-foreground",
                        it.state === "running" && "text-primary"
                      )}
                    >
                      {it.state === "running" && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                      {it.state === "done" && <CheckCircle2 className="h-3.5 w-3.5" />}
                      {it.state === "error" && <AlertTriangle className="h-3.5 w-3.5" />}
                      {it.state === "canceled" && <Ban className="h-3.5 w-3.5" />}
                      {it.state === "running" && pct !== null ? `${Math.floor(pct)}%` : t("transfers.state." + it.state)}
                    </span>
                    {it.state === "running" && (
                      <button
                        type="button"
                        onClick={() => cancel(it.id)}
                        className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-red-500"
                        title={t("upload.cancel")}
                        aria-label={`${t("upload.cancel")} ${it.name}`}
                      >
                        <X className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                  <div className="mt-2 h-1.5 overflow-hidden rounded bg-muted">
                    {pct === null ? (
                      <div className="h-full w-1/3 animate-loading-bar bg-primary" style={{ position: "relative" }} />
                    ) : (
                      <div
                        className={cn(
                          "h-full transition-all",
                          it.state === "error" ? "bg-red-500" : it.state === "canceled" ? "bg-muted-foreground/40" : it.state === "done" ? "bg-emerald-500" : "bg-primary"
                        )}
                        style={{ width: `${it.state === "error" ? 100 : pct}%` }}
                      />
                    )}
                  </div>
                  <div className="mt-1.5 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                    <span>
                      {humanSize(it.done)}
                      {it.total ? ` / ${humanSize(it.total)}` : ""}
                    </span>
                    {it.state === "running" && it.speed > 0 && <span>{humanSize(it.speed)}/s</span>}
                    {it.state === "running" && eta(it) && <span>{tf("transfers.eta", { t: eta(it) })}</span>}
                    {it.error && <span className="text-red-500">{it.error}</span>}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}

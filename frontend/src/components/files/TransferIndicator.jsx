import { Link, useLocation } from "react-router-dom";
import { ArrowDownToLine } from "lucide-react";
import { useTransfers } from "contexts/TransferContext";
import { useI18n } from "i18n";
import { cn, humanSize } from "lib/utils";

const R = 15;
const C = 2 * Math.PI * R;

/**
 * Header button for the transfers page. While something is running, a ring
 * around the icon shows the overall progress (like Firefox's downloads
 * button); unknown sizes (zip streams) spin.
 */
export default function TransferIndicator() {
  const { summary, items } = useTransfers();
  const { t, tf } = useI18n();
  const location = useLocation();
  const active = location.pathname === "/transfers";
  const running = summary.running > 0;
  const pct = summary.progress;

  const title = running
    ? tf("transfers.headerRunning", {
        n: summary.running,
        pct: pct === null ? "…" : `${Math.floor(pct * 100)}%`,
        speed: humanSize(summary.speed),
      })
    : t("transfers.title");

  return (
    <Link
      to="/transfers"
      title={title}
      aria-label={title}
      className={cn(
        "relative inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
        (active || running) && "text-foreground"
      )}
    >
      {running && (
        <svg className="absolute inset-0 h-9 w-9 -rotate-90" viewBox="0 0 36 36" aria-hidden="true">
          <circle cx="18" cy="18" r={R} fill="none" strokeWidth="2.5" className="stroke-muted" />
          {pct === null ? (
            <circle
              cx="18"
              cy="18"
              r={R}
              fill="none"
              strokeWidth="2.5"
              strokeLinecap="round"
              className="origin-center animate-spin stroke-primary"
              strokeDasharray={`${C * 0.25} ${C}`}
            />
          ) : (
            <circle
              cx="18"
              cy="18"
              r={R}
              fill="none"
              strokeWidth="2.5"
              strokeLinecap="round"
              className="stroke-primary transition-[stroke-dashoffset] duration-300"
              strokeDasharray={C}
              strokeDashoffset={C * (1 - pct)}
            />
          )}
        </svg>
      )}
      <ArrowDownToLine className={cn("relative h-4 w-4", running && "text-primary")} />
      {summary.running > 1 && (
        <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground">
          {summary.running}
        </span>
      )}
      {!running && summary.failed > 0 && items.length > 0 && (
        <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-red-500" />
      )}
    </Link>
  );
}

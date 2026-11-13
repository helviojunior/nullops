import { useEffect, useState } from "react";
import { Link, Navigate, Outlet, useLocation } from "react-router-dom";
import {
  LogOut,
  Moon,
  Sun,
  Languages,
  ShieldCheck,
  ShieldOff,
  Loader2,
  PanelLeftOpen,
  PanelLeftClose,
  UserRound,
} from "lucide-react";
import { useAuth } from "contexts/AuthContext";
import { TreeProvider } from "contexts/TreeContext";
import Logo from "components/brand/Logo";
import TransferIndicator from "components/files/TransferIndicator";
import { useTransfers } from "contexts/TransferContext";
import { useDialog } from "contexts/DialogContext";
import FolderTree, { TREE_DEFAULT, TREE_MAX, TREE_MIN } from "components/layout/FolderTree";
import { parentPath, routeToPath } from "lib/utils";
import { useI18n, LANGUAGE_OPTIONS } from "i18n";
import { Button } from "components/ui/button";

export function LanguageSelect({ className = "" }) {
  const { t, lang, setLanguage } = useI18n();
  return (
    <label className={`inline-flex items-center gap-1.5 text-sm text-muted-foreground ${className}`}>
      <Languages className="h-4 w-4" />
      <span className="sr-only">{t("common.language")}</span>
      <select
        value={lang}
        onChange={(e) => setLanguage(e.target.value)}
        className="bg-transparent text-foreground text-sm outline-none cursor-pointer"
      >
        {LANGUAGE_OPTIONS.map((o) => (
          <option key={o.value} value={o.value} className="bg-card">
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}

export function ThemeToggle({ darkMode, setDarkMode }) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      onClick={() => setDarkMode(!darkMode)}
      className="p-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
      title={t("common.theme")}
      aria-label={t("common.theme")}
    >
      {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  );
}

const PANEL_KEY = "nullops.tree";

function loadPanel() {
  try {
    const v = JSON.parse(localStorage.getItem(PANEL_KEY) || "{}");
    return {
      open: typeof v.open === "boolean" ? v.open : window.innerWidth >= 768,
      width: Math.min(TREE_MAX, Math.max(TREE_MIN, Number(v.width) || TREE_DEFAULT)),
      tab: v.tab === "pads" ? "pads" : "folders",
    };
  } catch {
    return { open: window.innerWidth >= 768, width: TREE_DEFAULT, tab: "folders" };
  }
}

// Folder being shown: /files/<dir> or the parent of /edit/<file>.
function currentFolder(pathname) {
  const m = pathname.match(/^\/(files|edit|view)(?:\/(.*))?$/);
  if (!m) return "";
  const p = routeToPath(m[2] || "");
  return m[1] === "files" ? p : parentPath(p);
}

export default function AppLayout({ darkMode, setDarkMode }) {
  const { user, status, logout, encrypted } = useAuth();
  const { t, tf } = useI18n();
  const location = useLocation();
  const [panel, setPanel] = useState(loadPanel);

  useEffect(() => {
    try {
      localStorage.setItem(PANEL_KEY, JSON.stringify(panel));
    } catch {
      /* ignore */
    }
  }, [panel]);

  const transfers = useTransfers();
  const { confirm } = useDialog();
  // signing out ends the session: running transfers would be cut
  const signOut = async () => {
    if (transfers.summary.running) {
      const ok = await confirm({
        title: t("transfers.logoutTitle"),
        description: tf("transfers.logoutDesc", { n: transfers.summary.running }),
        variant: "warning",
        confirmLabel: t("nav.logout"),
      });
      if (ok !== true) return;
      transfers.cancelAll();
    }
    logout();
  };
  const setWidth = (width) => setPanel((p) => ({ ...p, width }));
  const setTab = (tab) => setPanel((p) => ({ ...p, tab }));
  const padMatch = location.pathname.match(/^\/pad\/([^/]+)$/);
  const currentPad = padMatch ? decodeURIComponent(padMatch[1]) : null;
  // opening a pad shows the PastePad tab
  useEffect(() => {
    if (currentPad) setPanel((p) => (p.tab === "pads" ? p : { ...p, tab: "pads" }));
  }, [currentPad]);
  const togglePanel = () => setPanel((p) => ({ ...p, open: !p.open }));

  if (!user) {
    if (status === "connecting") {
      return (
        <div className="flex h-screen items-center justify-center text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      );
    }
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="flex h-screen flex-col">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-card px-4">
        <button
          type="button"
          onClick={togglePanel}
          className="-ml-1 rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
          title={panel.open ? t("tree.hide") : t("tree.show")}
          aria-label={panel.open ? t("tree.hide") : t("tree.show")}
          aria-pressed={panel.open}
        >
          {panel.open ? <PanelLeftClose className="h-5 w-5" /> : <PanelLeftOpen className="h-5 w-5" />}
        </button>
        <Logo className="h-7 shrink-0" />
        <span className="hidden truncate border-l border-border pl-3 text-sm font-medium text-muted-foreground sm:inline">
          {!user.title || user.title === t("app.name") ? t("app.product") : user.title}
        </span>
        {user.readonly && (
          <span className="rounded-full border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-xs text-amber-500">
            {t("nav.readonly")}
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <LanguageSelect className="hidden sm:inline-flex" />
          <TransferIndicator />
          <ThemeToggle darkMode={darkMode} setDarkMode={setDarkMode} />
          {user.auth && (
            <>
              <Link
                to="/account"
                title={t("account.title")}
                className="inline-flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <UserRound className="h-4 w-4" />
                <span className="hidden md:inline">{user.username}</span>
              </Link>
              <Button variant="ghost" size="sm" onClick={signOut} title={t("nav.logout")}>
                <LogOut className="h-4 w-4" />
                <span className="hidden sm:inline">{t("nav.logout")}</span>
              </Button>
            </>
          )}
        </div>
      </header>

      <TreeProvider>
        <div className="flex min-h-0 flex-1">
          {panel.open && (
            <FolderTree
              current={currentFolder(location.pathname)}
              currentPad={currentPad}
              tab={panel.tab}
              setTab={setTab}
              width={panel.width}
              setWidth={setWidth}
              onCollapse={togglePanel}
            />
          )}
          <main className="flex min-h-0 min-w-0 flex-1 flex-col">
            <Outlet />
          </main>
        </div>
      </TreeProvider>

      <footer className="flex h-8 shrink-0 items-center gap-3 border-t border-border bg-card px-4 text-xs text-muted-foreground">
        {encrypted ? (
          <>
            <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
            <span className="truncate">{t("app.secure")}</span>
          </>
        ) : (
          <>
            <ShieldOff className="h-3.5 w-3.5 text-red-500" />
            <span className="truncate text-red-500">{t("app.plain")}</span>
          </>
        )}
        <span className="ml-auto">{tf("footer.version", { v: user.version })}</span>
      </footer>
    </div>
  );
}

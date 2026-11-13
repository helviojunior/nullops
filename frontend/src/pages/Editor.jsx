import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useBlocker } from "react-router-dom";
import { ArrowLeft, Save, Loader2, Download } from "lucide-react";
import { useApi, useAuth } from "contexts/AuthContext";
import { useDialog } from "contexts/DialogContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { errorText } from "lib/errors";
import { baseName, parentPath, pathToRoute, routeToPath, saveBlob } from "lib/utils";

// Text editor: a dedicated route (/edit/<path>), never a modal.
export default function Editor() {
  const params = useParams();
  const path = routeToPath(params["*"]);
  const navigate = useNavigate();
  const run = useApi();
  const { user } = useAuth();
  const { alert, confirm } = useDialog();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const [content, setContent] = useState("");
  const [original, setOriginal] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const gutterRef = useRef(null);

  const dirty = content !== original;
  const readonly = !!user?.readonly;
  const back = pathToRoute("/files", parentPath(path));

  useEffect(() => {
    let alive = true;
    setLoading(true);
    run((c) => c.call("read", { path }))
      .then((r) => {
        if (!alive) return;
        setContent(r.content);
        setOriginal(r.content);
      })
      .catch(async (err) => {
        if (!alive || err.status === 401) return;
        await alert({ title: t("common.error"), description: errorText(err, lang), variant: "danger" });
        navigate(back, { replace: true });
      })
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  const save = useCallback(async () => {
    if (readonly || saving) return;
    setSaving(true);
    try {
      await run((c) => c.call("save", { path, content }));
      setOriginal(content);
      toast.success(t("toast.saved"));
    } catch (err) {
      if (err.status !== 401) toast.error(errorText(err, lang));
    } finally {
      setSaving(false);
    }
  }, [readonly, saving, run, path, content, toast, t, lang]);

  useEffect(() => {
    const onKey = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        save();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [save]);

  // Leaving with unsaved changes: own modal, not the browser's.
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && currentLocation.pathname !== nextLocation.pathname);
  useEffect(() => {
    if (blocker.state !== "blocked") return;
    confirm({
      title: t("editor.discardTitle"),
      description: t("editor.discardDesc"),
      variant: "warning",
      confirmLabel: t("editor.discard"),
    }).then((ok) => (ok ? blocker.proceed() : blocker.reset()));
  }, [blocker, confirm, t]);

  const download = async () => {
    try {
      const blob = await run((c) => c.download(path));
      saveBlob(blob, baseName(path));
    } catch (err) {
      if (err.status !== 401) toast.error(errorText(err, lang));
    }
  };

  const lines = content ? content.split("\n").length : 1;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Button variant="ghost" size="sm" onClick={() => navigate(back)}>
          <ArrowLeft className="h-4 w-4" /> {t("common.back")}
        </Button>
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm">/{path}</div>
          <div className="text-xs text-muted-foreground">
            {readonly ? t("editor.readonly") : dirty ? t("editor.unsaved") : t("editor.saved")} ·{" "}
            {tf("editor.lines", { n: lines })}
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={download}>
          <Download className="h-4 w-4" /> {t("action.download")}
        </Button>
        {!readonly && (
          <Button size="sm" onClick={save} loading={saving} disabled={!dirty}>
            <Save className="h-4 w-4" /> {t("common.save")}
          </Button>
        )}
      </div>
      {loading ? (
        <div className="flex flex-1 items-center justify-center text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 overflow-hidden font-mono text-sm">
          <div
            ref={gutterRef}
            aria-hidden="true"
            className="select-none overflow-hidden border-r border-border bg-card px-3 py-3 text-right text-muted-foreground"
          >
            {Array.from({ length: lines }, (_, i) => (
              <div key={i} className="leading-6">
                {i + 1}
              </div>
            ))}
          </div>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            onScroll={(e) => {
              if (gutterRef.current) gutterRef.current.scrollTop = e.target.scrollTop;
            }}
            readOnly={readonly}
            spellCheck={false}
            wrap="off"
            className="min-h-0 w-full flex-1 resize-none bg-background p-3 leading-6 text-foreground outline-none"
          />
        </div>
      )}
    </div>
  );
}

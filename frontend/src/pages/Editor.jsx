import { useCallback, useEffect, useState } from "react";
import { useNavigate, useBlocker } from "react-router-dom";
import { useRoutePath } from "lib/useRoutePath";
import { ArrowLeft, Save, Loader2, Download, Eye } from "lucide-react";
import { useApi, useAuth } from "contexts/AuthContext";
import { useDialog } from "contexts/DialogContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import CodeEditor from "components/files/CodeEditor";
import { errorText } from "lib/errors";
import { useDark } from "lib/useDark";
import { baseName, parentPath, pathToRoute, saveBlob } from "lib/utils";

const ENC_LABEL = { "utf-8": "UTF-8", "utf-8-bom": "UTF-8 BOM", "utf-16le": "UTF-16 LE", "utf-16be": "UTF-16 BE" };

// Text editor (CodeMirror): a dedicated route (/edit/<path>), never a modal.
// Encoding and line endings of the file are preserved on save; saving fails
// with a conflict when the file changed on disk since it was opened.
export default function Editor() {
  const { path, ready } = useRoutePath("/edit");
  const navigate = useNavigate();
  const run = useApi();
  const { user } = useAuth();
  const { alert, confirm } = useDialog();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const dark = useDark();
  const [doc, setDoc] = useState(null); // {content, encoding, eol, hash}
  const [content, setContent] = useState("");
  const [original, setOriginal] = useState("");
  const [docKey, setDocKey] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const dirty = content !== original;
  const readonly = !!user?.readonly;
  const back = pathToRoute("/files", parentPath(path || ""));

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const d = await run((c) => c.call("read", { path }));
      setDoc(d);
      setContent(d.content);
      setOriginal(d.content);
      setDocKey((k) => k + 1);
    } catch (err) {
      if (err.status === 401) return;
      await alert({ title: t("common.error"), description: errorText(err, lang), variant: "danger" });
      navigate(back, { replace: true });
    } finally {
      setLoading(false);
    }
  }, [run, path, alert, t, lang, navigate, back]);

  useEffect(() => {
    if (ready) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, ready]);

  const save = useCallback(
    async (force = false) => {
      if (readonly || saving || !doc) return;
      setSaving(true);
      try {
        const r = await run((c) =>
          c.call("save", { path, content, encoding: doc.encoding, eol: doc.eol, hash: doc.hash, force })
        );
        setDoc((d) => ({ ...d, hash: r.hash }));
        setOriginal(content);
        toast.success(t("toast.saved"));
      } catch (err) {
        if (err.status === 401) return;
        if (err.code === "conflict") {
          setSaving(false);
          const choice = await confirm({
            title: t("editor.conflictTitle"),
            description: t("editor.conflictDesc"),
            variant: "warning",
            confirmLabel: t("editor.overwrite"),
            extraLabel: t("editor.reload"),
          });
          if (choice === true) return save(true);
          if (choice === "extra") return load();
          return;
        }
        toast.error(errorText(err, lang));
      } finally {
        setSaving(false);
      }
    },
    [readonly, saving, doc, run, path, content, toast, t, lang, confirm, load]
  );

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
    }).then((ok) => (ok === true ? blocker.proceed() : blocker.reset()));
  }, [blocker, confirm, t]);

  const download = async () => {
    try {
      saveBlob(await run((c) => c.download(path)), baseName(path));
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
            {doc && ` · ${ENC_LABEL[doc.encoding] || doc.encoding} · ${doc.eol.toUpperCase()}`}
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={() => navigate(pathToRoute("/view", path))}>
          <Eye className="h-4 w-4" /> {t("action.view")}
        </Button>
        <Button variant="outline" size="sm" onClick={download}>
          <Download className="h-4 w-4" /> {t("action.download")}
        </Button>
        {!readonly && (
          <Button size="sm" onClick={() => save()} loading={saving} disabled={!dirty}>
            <Save className="h-4 w-4" /> {t("common.save")}
          </Button>
        )}
      </div>
      {loading && !doc ? (
        <div className="flex flex-1 items-center justify-center text-muted-foreground">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : (
        <div className="min-h-0 flex-1">
          <CodeEditor
            value={original}
            docKey={docKey}
            fileName={baseName(path)}
            readOnly={readonly}
            dark={dark}
            onChange={setContent}
          />
        </div>
      )}
    </div>
  );
}

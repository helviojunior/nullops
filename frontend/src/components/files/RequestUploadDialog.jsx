import { useCallback, useEffect, useState } from "react";
import { Check, Copy, Link2, Loader2, Trash2, UploadCloud } from "lucide-react";
import { Modal } from "components/ui/modal";
import { Button } from "components/ui/button";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { errorText } from "lib/errors";

const linkOf = (token) => `${window.location.origin}/public/upload/${token}`;

function until(iso, lang) {
  const mins = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000));
  const rtf = new Intl.RelativeTimeFormat(lang === "pt-br" ? "pt-BR" : "en", { numeric: "auto" });
  return mins >= 60 ? rtf.format(Math.round(mins / 6) / 10, "hour") : rtf.format(mins, "minute");
}

/**
 * "Request upload": creates a public link (valid 3 h, unlimited uses) that
 * lets anyone upload files into this folder, and lists the active ones.
 */
export default function RequestUploadDialog({ open, path, onClose }) {
  const run = useApi();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const [created, setCreated] = useState(null);
  const [links, setLinks] = useState([]);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      setLinks(await run((c) => c.call("uploadreq_list", { path })));
    } catch {
      setLinks([]);
    }
  }, [run, path]);

  // opening the dialog creates the request right away
  useEffect(() => {
    if (!open) return;
    setCreated(null);
    setError("");
    setBusy(true);
    run((c) => c.call("uploadreq_create", { path }))
      .then((r) => setCreated(r))
      .catch((e) => setError(errorText(e, lang)))
      .finally(() => {
        setBusy(false);
        load();
      });
  }, [open, path, run, load, lang]);

  const copy = async (token) => {
    try {
      await navigator.clipboard.writeText(linkOf(token));
      setCopied(token);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* clipboard unavailable: the link is selectable */
    }
  };

  const revoke = async (token) => {
    try {
      await run((c) => c.call("uploadreq_revoke", { token }));
      toast.success(t("reqUpload.revoked"));
      if (created?.token === token) setCreated(null);
      load();
    } catch (e) {
      toast.error(errorText(e, lang));
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t("reqUpload.title")}
      icon={
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-blue-100 dark:bg-blue-900/30">
          <UploadCloud className="h-5 w-5 text-blue-600 dark:text-blue-400" />
        </div>
      }
      size="xl"
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      <div className="space-y-4 text-left">
        <p className="text-sm text-muted-foreground">{tf("reqUpload.desc", { path: "/" + path })}</p>
        {busy && (
          <div className="flex justify-center p-4 text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin" />
          </div>
        )}
        {error && <p className="text-sm text-red-500">{error}</p>}
        {created && (
          <div className="space-y-2 rounded-lg border border-primary/40 bg-primary/5 p-3">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("reqUpload.newLink")}</div>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 select-all truncate rounded bg-background px-2 py-1.5 font-mono text-sm">
                {linkOf(created.token)}
              </code>
              <Button size="sm" onClick={() => copy(created.token)}>
                {copied === created.token ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {copied === created.token ? t("common.copied") : t("common.copy")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {tf("reqUpload.validity", { when: until(created.expires_at, lang) })}
            </p>
          </div>
        )}
        {links.length > 0 && (
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("reqUpload.active")}</div>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {links.map((l) => (
                <li key={l.token} className="flex items-center gap-2 px-3 py-2 text-sm">
                  <Link2 className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="font-mono">{l.token}</span>
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                    {tf("reqUpload.linkInfo", { user: l.created_by, when: until(l.expires_at, lang), n: l.uploads })}
                  </span>
                  <button
                    type="button"
                    onClick={() => copy(l.token)}
                    className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                    title={t("common.copy")}
                  >
                    {copied === l.token ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  </button>
                  <button
                    type="button"
                    onClick={() => revoke(l.token)}
                    className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-red-500"
                    title={t("reqUpload.revoke")}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </Modal>
  );
}

import { useCallback, useEffect, useState } from "react";
import { Check, Copy, Link2, Loader2, Share2, Trash2 } from "lucide-react";
import { Modal } from "components/ui/modal";
import { Button } from "components/ui/button";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { errorText } from "lib/errors";

// The /share route is a plain, direct download; the link is just origin + the
// opaque path the server returns (/share/<uuid>?token=<token>).
const fullLink = (url) => `${window.location.origin}${url}`;
const linkOf = (l) => `${window.location.origin}/share/${l.id}?token=${encodeURIComponent(l.token)}`;

const TTL_OPTIONS = [
  { key: "share.ttl.1h", secs: 3600 },
  { key: "share.ttl.24h", secs: 24 * 3600 },
  { key: "share.ttl.7d", secs: 7 * 24 * 3600 },
  { key: "share.ttl.30d", secs: 30 * 24 * 3600 },
];

function until(iso, lang) {
  const mins = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 60000));
  const rtf = new Intl.RelativeTimeFormat(lang === "pt-br" ? "pt-BR" : "en", { numeric: "auto" });
  if (mins >= 1440) return rtf.format(Math.round(mins / 144) / 10, "day");
  if (mins >= 60) return rtf.format(Math.round(mins / 6) / 10, "hour");
  return rtf.format(mins, "minute");
}

/**
 * "Share file": creates a public, opaque, direct-download link
 * (/share/<uuid>?token=<token>) for one file, valid for the chosen duration,
 * and lists / revokes the active ones.
 */
export default function ShareDialog({ open, entry, onClose }) {
  const run = useApi();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const path = entry?.path || "";
  const name = entry?.name || "";
  const [ttl, setTtl] = useState(24 * 3600);
  const [created, setCreated] = useState(null);
  const [links, setLinks] = useState([]);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      setLinks(await run((c) => c.call("share_list", { path })));
    } catch {
      setLinks([]);
    }
  }, [run, path]);

  // reset and load the active links whenever the dialog opens for a file
  useEffect(() => {
    if (!open) return;
    setCreated(null);
    setError("");
    setTtl(24 * 3600);
    load();
  }, [open, path, load]);

  const create = async () => {
    setBusy(true);
    setError("");
    try {
      setCreated(await run((c) => c.call("share_create", { path, ttl })));
      load();
    } catch (e) {
      setError(errorText(e, lang));
    } finally {
      setBusy(false);
    }
  };

  const copy = async (text, key) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* clipboard unavailable: the link is selectable */
    }
  };

  const revoke = async (id) => {
    try {
      await run((c) => c.call("share_revoke", { id }));
      toast.success(t("share.revoked"));
      if (created?.id === id) setCreated(null);
      load();
    } catch (e) {
      toast.error(errorText(e, lang));
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t("share.title")}
      icon={
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900/30">
          <Share2 className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
        </div>
      }
      size="xl"
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      <div className="space-y-4 text-left">
        <p className="text-sm text-muted-foreground">{tf("share.desc", { name })}</p>

        {!created && (
          <div className="flex flex-wrap items-end gap-2">
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {t("share.validity")}
              </span>
              <select
                value={ttl}
                onChange={(e) => setTtl(Number(e.target.value))}
                className="rounded-md border border-border bg-background px-2 py-1.5 text-sm"
              >
                {TTL_OPTIONS.map((o) => (
                  <option key={o.key} value={o.secs}>
                    {t(o.key)}
                  </option>
                ))}
              </select>
            </label>
            <Button onClick={create} disabled={busy}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}
              {t("share.create")}
            </Button>
          </div>
        )}

        {error && <p className="text-sm text-red-500">{error}</p>}

        {created && (
          <div className="space-y-2 rounded-lg border border-primary/40 bg-primary/5 p-3">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("share.newLink")}</div>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 select-all truncate rounded bg-background px-2 py-1.5 font-mono text-sm">
                {fullLink(created.url)}
              </code>
              <Button size="sm" onClick={() => copy(fullLink(created.url), created.id)}>
                {copied === created.id ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {copied === created.id ? t("common.copied") : t("common.copy")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{tf("share.expires", { when: until(created.expires_at, lang) })}</p>
          </div>
        )}

        {links.length > 0 && (
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("share.active")}</div>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {links.map((l) => (
                <li key={l.id} className="flex items-center gap-2 px-3 py-2 text-sm">
                  <Link2 className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                    {tf("share.linkInfo", { user: l.created_by, when: until(l.expires_at, lang), n: l.downloads })}
                  </span>
                  <button
                    type="button"
                    onClick={() => copy(linkOf(l), l.id)}
                    className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
                    title={t("common.copy")}
                  >
                    {copied === l.id ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  </button>
                  <button
                    type="button"
                    onClick={() => revoke(l.id)}
                    className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-red-500"
                    title={t("share.revoke")}
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

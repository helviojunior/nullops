import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { FileText, Loader2, Plus, Trash2, Users } from "lucide-react";
import { useApi } from "contexts/AuthContext";
import { useDialog } from "contexts/DialogContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { ActionMenu } from "components/ui/menu";
import { errorText } from "lib/errors";
import { cn } from "lib/utils";

export const PAD_NAME_RE = /^[A-Za-z0-9._-]{1,64}$/;
export const validPadName = (n) => PAD_NAME_RE.test(n) && n !== "." && n !== "..";

function timeAgo(iso, lang) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  const rtf = new Intl.RelativeTimeFormat(lang === "pt-br" ? "pt-BR" : "en", { numeric: "auto" });
  if (s < 60) return rtf.format(-Math.round(s), "second");
  if (s < 3600) return rtf.format(-Math.round(s / 60), "minute");
  if (s < 86400) return rtf.format(-Math.round(s / 3600), "hour");
  return rtf.format(-Math.round(s / 86400), "day");
}

/** PastePad tab of the side panel: list, create and delete pads. */
export default function PadList({ current }) {
  const run = useApi();
  const navigate = useNavigate();
  const { prompt, confirm } = useDialog();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const [pads, setPads] = useState(null);

  const load = useCallback(async () => {
    try {
      setPads(await run((c) => c.call("pad_list", {})));
    } catch (e) {
      if (e.status !== 401) setPads((p) => p || []);
    }
  }, [run]);

  useEffect(() => {
    load();
    const id = setInterval(load, 10000);
    window.addEventListener("nullops:pads", load);
    return () => {
      clearInterval(id);
      window.removeEventListener("nullops:pads", load);
    };
  }, [load]);

  const create = () =>
    prompt({
      title: t("pad.new"),
      label: t("pad.name"),
      hint: t("pad.nameHint"),
      confirmLabel: t("common.create"),
      validate: (v) => (validPadName(v.trim()) ? "" : t("pad.invalidName")),
      onSubmit: async (v) => {
        await run((c) => c.call("pad_create", { name: v.trim() })).catch((e) => {
          throw new Error(errorText(e, lang));
        });
      },
    }).then((v) => {
      if (v === null) return;
      load();
      navigate(`/pad/${encodeURIComponent(v.trim())}`);
    });

  const remove = (name) =>
    confirm({
      title: tf("pad.deleteTitle", { name }),
      description: t("pad.deleteDesc"),
      variant: "danger",
      confirmLabel: t("action.delete"),
      onConfirm: () =>
        run((c) => c.call("pad_delete", { name })).catch((e) => {
          throw new Error(errorText(e, lang));
        }),
    }).then((ok) => {
      if (ok !== true) return;
      toast.success(tf("pad.deleted", { name }));
      if (current === name) navigate("/files");
      load();
    });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="p-2">
        <button
          type="button"
          onClick={create}
          className="flex w-full items-center justify-center gap-1.5 rounded-md border border-dashed border-border px-2 py-1.5 text-sm text-muted-foreground hover:border-primary hover:text-foreground"
        >
          <Plus className="h-4 w-4" /> {t("pad.new")}
        </button>
      </div>
      <ul className="min-h-0 flex-1 overflow-auto px-1.5 pb-1.5 scrollbar-thin">
        {pads === null && (
          <li className="flex justify-center p-4 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
          </li>
        )}
        {pads?.length === 0 && <li className="p-3 text-center text-xs text-muted-foreground">{t("pad.empty")}</li>}
        {pads?.map((p) => (
          <li key={p.name}>
            <div
              className={cn(
                "group flex items-center gap-1 rounded-md pr-1 text-sm",
                current === p.name ? "bg-primary/15 text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
              )}
            >
              <Link to={`/pad/${encodeURIComponent(p.name)}`} className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5" title={p.name}>
                <FileText className="h-4 w-4 shrink-0 text-sky-500" />
                <span className="min-w-0 flex-1">
                  <span className={cn("block truncate", current === p.name && "font-medium")}>{p.name}</span>
                  <span className="block truncate text-[11px] text-muted-foreground">
                    {timeAgo(p.updated_at, lang)}
                    {p.updated_by && ` · ${p.updated_by}`}
                  </span>
                </span>
                {p.editors?.length > 0 && (
                  <span className="inline-flex items-center gap-0.5 text-[11px] text-emerald-500" title={p.editors.join(", ")}>
                    <Users className="h-3 w-3" />
                    {p.editors.length}
                  </span>
                )}
              </Link>
              <ActionMenu
                title={t("browser.actions")}
                items={[{ label: t("action.delete"), icon: Trash2, onClick: () => remove(p.name), danger: true }]}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

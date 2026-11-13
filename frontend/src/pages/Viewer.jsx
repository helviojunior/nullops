import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useRoutePath } from "lib/useRoutePath";
import { ArrowLeft, ChevronLeft, ChevronRight, Download, FileWarning, Loader2, SquarePen } from "lucide-react";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { errorText } from "lib/errors";
import {
  PREVIEW_MAX_MEDIA,
  PREVIEW_MAX_TEXT,
  baseName,
  humanSize,
  isPreviewable,
  parentPath,
  pathToRoute,
  previewKind,
  saveBlob,
  sortEntries,
} from "lib/utils";

/**
 * Preview of images, PDFs and small text files on its own route
 * (/view/<path>), with previous/next navigation inside the folder.
 * Content is fetched through the encrypted download and shown from a blob URL.
 */
export default function Viewer() {
  const { path, ready } = useRoutePath("/view");
  const dir = parentPath(path || "");
  const navigate = useNavigate();
  const run = useApi();
  const toast = useToast();
  const { t, tf, lang } = useI18n();

  const [info, setInfo] = useState(null); // probe result
  const [url, setUrl] = useState(null);
  const [text, setText] = useState(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [siblings, setSiblings] = useState([]);

  // previewable files of the same folder, for previous / next
  useEffect(() => {
    if (!ready) return;
    run((c) => c.call("list", { path: dir }))
      .then((l) => setSiblings(sortEntries(l.entries, "name", "asc").filter((e) => !e.is_dir && isPreviewable(e.name))))
      .catch(() => setSiblings([]));
  }, [run, dir, ready]);

  useEffect(() => {
    let alive = true;
    let objectUrl = null;
    setLoading(true);
    setError("");
    setUrl(null);
    setText(null);
    if (!ready) return undefined;
    (async () => {
      try {
        const p = await run((c) => c.call("probe", { path }));
        if (!alive) return;
        const kind = previewKind(p.mime, p.is_text);
        setInfo({ ...p, kind });
        if (kind === "text") {
          if (p.size > PREVIEW_MAX_TEXT) return;
          const doc = await run((c) => c.call("read", { path }));
          if (alive) setText(doc.content);
        } else if (kind === "image" || kind === "pdf") {
          if (p.size > PREVIEW_MAX_MEDIA) return;
          const blob = await run((c) => c.download(path));
          const typed = new Blob([blob], { type: p.mime.split(";")[0] });
          objectUrl = URL.createObjectURL(typed);
          if (alive) setUrl(objectUrl);
        }
      } catch (e) {
        if (alive && e.status !== 401) setError(errorText(e, lang));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [run, path, ready, lang]);

  const index = useMemo(() => siblings.findIndex((e) => e.path === path), [siblings, path]);
  const go = useCallback(
    (delta) => {
      if (index < 0 || !siblings.length) return;
      const next = siblings[(index + delta + siblings.length) % siblings.length];
      navigate(pathToRoute("/view", next.path), { replace: true });
    },
    [index, siblings, navigate]
  );

  useEffect(() => {
    const onKey = (e) => {
      if (e.target.closest?.("input, textarea")) return;
      if (e.key === "ArrowLeft") go(-1);
      if (e.key === "ArrowRight") go(1);
      if (e.key === "Escape") navigate(pathToRoute("/files", dir));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, navigate, dir]);

  const download = async () => {
    try {
      saveBlob(await run((c) => c.download(path)), baseName(path));
    } catch (e) {
      if (e.status !== 401) toast.error(errorText(e, lang));
    }
  };

  const tooBig =
    info &&
    ((info.kind === "text" && info.size > PREVIEW_MAX_TEXT) ||
      ((info.kind === "image" || info.kind === "pdf") && info.size > PREVIEW_MAX_MEDIA));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Button variant="ghost" size="sm" onClick={() => navigate(pathToRoute("/files", dir))}>
          <ArrowLeft className="h-4 w-4" /> {t("common.back")}
        </Button>
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm">/{path}</div>
          <div className="text-xs text-muted-foreground">
            {info ? `${info.mime.split(";")[0]} · ${humanSize(info.size)}` : " "}
            {index >= 0 && siblings.length > 1 && ` · ${tf("viewer.position", { i: index + 1, n: siblings.length })}`}
          </div>
        </div>
        {siblings.length > 1 && (
          <>
            <Button variant="outline" size="sm" onClick={() => go(-1)} title={t("viewer.prev")}>
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <Button variant="outline" size="sm" onClick={() => go(1)} title={t("viewer.next")}>
              <ChevronRight className="h-4 w-4" />
            </Button>
          </>
        )}
        {info?.is_text && (
          <Button variant="outline" size="sm" onClick={() => navigate(pathToRoute("/edit", path))}>
            <SquarePen className="h-4 w-4" /> {t("action.edit")}
          </Button>
        )}
        <Button size="sm" onClick={download}>
          <Download className="h-4 w-4" /> {t("action.download")}
        </Button>
      </div>

      <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto bg-muted/30 p-4">
        {loading ? (
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        ) : error ? (
          <p className="text-sm text-red-500">{error}</p>
        ) : tooBig || info?.kind === "other" ? (
          <div className="flex flex-col items-center gap-3 text-center text-sm text-muted-foreground">
            <FileWarning className="h-10 w-10" />
            <p>{tooBig ? tf("viewer.tooBig", { size: humanSize(info.size) }) : t("viewer.unsupported")}</p>
            <Button onClick={download}>
              <Download className="h-4 w-4" /> {t("action.download")}
            </Button>
          </div>
        ) : info?.kind === "image" && url ? (
          <img src={url} alt={baseName(path)} className="max-h-full max-w-full object-contain shadow" />
        ) : info?.kind === "pdf" && url ? (
          <iframe src={url} title={baseName(path)} className="h-full w-full rounded border border-border bg-white" />
        ) : text !== null ? (
          <pre className="h-full w-full overflow-auto rounded border border-border bg-background p-4 font-mono text-sm leading-6">
            {text}
          </pre>
        ) : null}
      </div>
    </div>
  );
}


import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { AlertTriangle, CheckCircle2, Loader2, ShieldAlert, UploadCloud, X } from "lucide-react";
import { ApiClient, isAbort } from "lib/api";
import { InsecureContextError } from "lib/crypto";
import { errorText } from "lib/errors";
import { displayName, humanSize } from "lib/utils";
import { useI18n } from "i18n";
import { Card, CardContent, CardHeader } from "components/ui/card";
import Logo from "components/brand/Logo";
import { LanguageSelect } from "components/layout/AppLayout";

/**
 * Public upload page (/public/upload/<token>): no account needed. Files are
 * sent over the same encrypted channel (anonymous session) into the folder
 * chosen by whoever created the link; they never replace existing files.
 */
export default function PublicUpload() {
  const { token } = useParams();
  const { t, tf, lang } = useI18n();
  const clientRef = useRef(null);
  const [state, setState] = useState("loading"); // loading | ready | invalid | insecure | error
  const [info, setInfo] = useState(null);
  const [message, setMessage] = useState("");
  const [items, setItems] = useState([]);
  const [dragging, setDragging] = useState(false);
  const input = useRef(null);
  const seq = useRef(0);
  const queue = useRef(Promise.resolve());

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const c = new ApiClient();
        await c.connect();
        const i = await c.call("public_upload_info", { token });
        if (!alive) return;
        clientRef.current = c;
        setInfo(i);
        setState("ready");
      } catch (e) {
        if (!alive) return;
        if (e instanceof InsecureContextError) setState("insecure");
        else {
          setMessage(errorText(e, lang));
          setState(e.status === 200 ? "invalid" : "error");
        }
      }
    })();
    return () => {
      alive = false;
    };
  }, [token, lang]);

  const update = (id, patch) => setItems((all) => all.map((x) => (x.id === id ? { ...x, ...patch } : x)));

  const send = useCallback(
    (files) => {
      for (const file of files) {
        const id = ++seq.current;
        const ctrl = new AbortController();
        setItems((all) => [{ id, name: displayName(file.name), total: file.size, done: 0, state: "waiting", cancel: () => ctrl.abort() }, ...all]);
        // one file at a time, in order
        queue.current = queue.current.then(async () => {
          if (ctrl.signal.aborted) return update(id, { state: "canceled" });
          update(id, { state: "running" });
          let last = 0;
          try {
            const res = await clientRef.current.upload("", file, (done, total) => {
              const now = Date.now();
              if (now - last < 250 && done < total) return;
              last = now;
              update(id, { done });
            }, ctrl.signal, { action: "public_upload", extra: { token } });
            // the name the server stored (sanitized, name-<timestamp>.ext)
            update(id, { state: "done", done: file.size, stored: res?.name });
          } catch (e) {
            update(id, isAbort(e) ? { state: "canceled" } : { state: "error", error: errorText(e, lang) });
          }
        });
      }
    },
    [token, lang]
  );

  const running = items.some((x) => x.state === "running" || x.state === "waiting");
  useEffect(() => {
    if (!running) return undefined;
    const warn = (e) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [running]);

  const drop = {
    onDragEnter: (e) => {
      e.preventDefault();
      setDragging(true);
    },
    onDragOver: (e) => e.preventDefault(),
    onDragLeave: (e) => {
      if (!e.currentTarget.contains(e.relatedTarget)) setDragging(false);
    },
    onDrop: (e) => {
      e.preventDefault();
      setDragging(false);
      if (state === "ready") send(Array.from(e.dataTransfer.files || []));
    },
  };

  return (
    <div className="flex min-h-screen flex-col bg-background">
      <div className="flex justify-end p-4">
        <LanguageSelect />
      </div>
      <div className="flex flex-1 justify-center p-4 md:pt-[6vh]">
        <Card className="w-full max-w-2xl self-start animate-fade-in">
          <CardHeader className="items-center text-center">
            <Logo className="mb-2 h-12" />
            <h1 className="text-lg font-semibold">{t("publicUpload.title")}</h1>
            {info && (
              <p className="text-sm text-muted-foreground">
                {tf("publicUpload.subtitle", {
                  user: info.requested_by,
                  when: new Date(info.expires_at).toLocaleString(lang === "pt-br" ? "pt-BR" : "en"),
                })}
              </p>
            )}
          </CardHeader>
          <CardContent className="space-y-4">
            {state === "loading" && (
              <div className="flex justify-center p-8 text-muted-foreground">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            )}
            {(state === "invalid" || state === "error" || state === "insecure") && (
              <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm">
                <div className="mb-1 flex items-center gap-2 font-medium text-red-500">
                  <ShieldAlert className="h-4 w-4" />
                  {state === "insecure" ? t("login.insecureTitle") : t("publicUpload.invalidTitle")}
                </div>
                <p className="text-muted-foreground">{state === "insecure" ? t("login.insecureHint") : message}</p>
              </div>
            )}
            {state === "ready" && (
              <>
                <div
                  {...drop}
                  role="button"
                  tabIndex={0}
                  onClick={() => input.current?.click()}
                  onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && input.current?.click()}
                  className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed p-10 text-center transition-colors ${
                    dragging ? "border-primary bg-primary/10" : "border-border hover:border-primary/60 hover:bg-muted/40"
                  }`}
                >
                  <UploadCloud className="h-10 w-10 text-primary" />
                  <div className="font-medium">{t("publicUpload.drop")}</div>
                  <div className="text-xs text-muted-foreground">
                    {t("publicUpload.hint")}
                    {info?.max_size > 0 && ` · ${tf("publicUpload.max", { size: humanSize(info.max_size) })}`}
                  </div>
                  <input
                    ref={input}
                    type="file"
                    multiple
                    className="hidden"
                    onChange={(e) => {
                      send(Array.from(e.target.files || []));
                      e.target.value = "";
                    }}
                  />
                </div>
                {items.length > 0 && (
                  <ul className="space-y-2">
                    {items.map((it) => {
                      const pct = it.total ? Math.min(100, (it.done / it.total) * 100) : it.state === "done" ? 100 : 0;
                      return (
                        <li key={it.id} className="rounded-lg border border-border p-3">
                          <div className="flex items-center gap-2 text-sm">
                            <span className="min-w-0 flex-1 truncate font-medium" title={it.name}>
                              {it.name}
                            </span>
                            {it.state === "done" && <CheckCircle2 className="h-4 w-4 text-emerald-500" />}
                            {it.state === "error" && <AlertTriangle className="h-4 w-4 text-red-500" />}
                            <span className="text-xs text-muted-foreground">
                              {it.state === "running" ? `${Math.floor(pct)}%` : t("publicUpload.state." + it.state)}
                            </span>
                            {(it.state === "running" || it.state === "waiting") && (
                              <button
                                type="button"
                                onClick={it.cancel}
                                className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-red-500"
                                title={t("upload.cancel")}
                              >
                                <X className="h-4 w-4" />
                              </button>
                            )}
                          </div>
                          <div className="mt-2 h-1.5 overflow-hidden rounded bg-muted">
                            <div
                              className={`h-full transition-all ${
                                it.state === "error" ? "bg-red-500" : it.state === "done" ? "bg-emerald-500" : "bg-primary"
                              }`}
                              style={{ width: `${it.state === "error" ? 100 : pct}%` }}
                            />
                          </div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            {it.error || `${humanSize(it.done)} / ${humanSize(it.total)}`}
                            {it.stored && ` · ${tf("publicUpload.storedAs", { name: it.stored })}`}
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

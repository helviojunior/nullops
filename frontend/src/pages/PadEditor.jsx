import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { EditorView, basicSetup } from "codemirror";
import { Annotation, Compartment, EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { CloudOff, Copy, FileText, Loader2, RefreshCw, Users, CheckCircle2 } from "lucide-react";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { PadClient } from "lib/padClient";
import { TextOperation } from "lib/ot";
import { errorText } from "lib/errors";
import { useDark } from "lib/useDark";

const remote = Annotation.define();

const theme = EditorView.theme({
  "&": { height: "100%", fontSize: "14px" },
  ".cm-scroller": { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" },
});

/**
 * PastePad editor (/pad/<name>): plain text edited by every signed-in user at
 * the same time. Local edits become OT operations; remote ones arrive through
 * a long poll and are merged into the editor without moving the cursor.
 */
export default function PadEditor() {
  const { name } = useParams();
  const navigate = useNavigate();
  const run = useApi();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const dark = useDark();
  const host = useRef(null);
  const viewRef = useRef(null);
  const themeComp = useRef(new Compartment());
  const darkRef = useRef(dark);
  darkRef.current = dark;
  const [status, setStatus] = useState("loading"); // loading | synced | saving | offline
  const [editors, setEditors] = useState([]);
  const [error, setError] = useState("");
  const [chars, setChars] = useState(0);

  useEffect(() => {
    let alive = true;
    const call = (action, payload) => run((c) => c.call(action, payload));
    const client = new PadClient(name, call, {
      applyRemote: (op) => {
        const v = viewRef.current;
        if (!v) return;
        v.dispatch({ changes: op.toChanges(), annotations: [remote.of(true)] });
      },
      resync: (text, err) => {
        const v = viewRef.current;
        if (!v) return;
        v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: text }, annotations: [remote.of(true)] });
        if (err) toast.error(errorText(err, lang));
        else toast.success(t("pad.resynced"));
      },
      status: (s) => alive && setStatus(s),
      editors: (list) => alive && setEditors(list),
      error: (err) => {
        if (!alive || err.status === 401) return;
        setError(errorText(err, lang));
      },
    });

    setStatus("loading");
    setError("");
    client
      .open()
      .then((text) => {
        if (!alive) return;
        const view = new EditorView({
          parent: host.current,
          state: EditorState.create({
            doc: text,
            extensions: [
              basicSetup,
              EditorView.lineWrapping,
              theme,
              themeComp.current.of(darkRef.current ? oneDark : []),
              EditorView.updateListener.of((u) => {
                if (!u.docChanged) return;
                setChars(u.state.doc.length);
                for (const tr of u.transactions) {
                  if (!tr.docChanged || tr.annotation(remote)) continue;
                  client.local(TextOperation.fromChanges(tr.changes, tr.startState.doc.length));
                }
              }),
            ],
          }),
        });
        viewRef.current = view;
        setChars(text.length);
        setStatus("synced");
        view.focus();
        client.start();
      })
      .catch((e) => {
        if (alive && e.status !== 401) {
          setError(errorText(e, lang));
          setStatus("offline");
        }
      });

    return () => {
      alive = false;
      client.stop();
      viewRef.current?.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [name]);

  useEffect(() => {
    viewRef.current?.dispatch({ effects: themeComp.current.reconfigure(dark ? oneDark : []) });
  }, [dark]);

  const copyAll = async () => {
    try {
      await navigator.clipboard.writeText(viewRef.current?.state.doc.toString() || "");
      toast.success(t("common.copied"));
    } catch {
      /* clipboard unavailable */
    }
  };

  const StatusIcon = { synced: CheckCircle2, saving: RefreshCw, offline: CloudOff, loading: Loader2 }[status];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <FileText className="h-5 w-5 text-sky-500" />
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm">{name}</div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <StatusIcon
              className={`h-3.5 w-3.5 ${status === "synced" ? "text-emerald-500" : status === "offline" ? "text-red-500" : "animate-spin"}`}
            />
            {t("pad.status." + status)} · {tf("pad.chars", { n: chars })}
          </div>
        </div>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground" title={editors.join(", ")}>
          <Users className="h-4 w-4" />
          {editors.length ? (
            <div className="flex flex-wrap gap-1">
              {editors.map((u) => (
                <span key={u} className="rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-emerald-500">
                  {u}
                </span>
              ))}
            </div>
          ) : (
            "—"
          )}
        </div>
        <Button variant="outline" size="sm" onClick={copyAll}>
          <Copy className="h-4 w-4" /> {t("common.copy")}
        </Button>
      </div>
      {error ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm">
          <p className="text-red-500">{error}</p>
          <Button variant="outline" size="sm" onClick={() => navigate("/files")}>
            {t("common.back")}
          </Button>
        </div>
      ) : (
        <div className="relative min-h-0 flex-1">
          {status === "loading" && (
            <div className="absolute inset-0 flex items-center justify-center text-muted-foreground">
              <Loader2 className="h-6 w-6 animate-spin" />
            </div>
          )}
          <div ref={host} className="h-full" />
        </div>
      )}
    </div>
  );
}

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { isAbort } from "lib/api";
import { errorText } from "lib/errors";
import { notifyChanged } from "lib/useTransfer";
import { baseName, saveBlob } from "lib/utils";

/**
 * Downloads and uploads, kept above the pages so they keep running while the
 * user browses, and shown by the header icon (overall progress ring) and the
 * /transfers page.
 *
 * item: { id, kind: "download"|"upload", name, path, state: "running"|"done"|
 *         "error"|"canceled", done, total, speed (B/s), error, startedAt, endedAt }
 */
const TransferContext = createContext(null);

// files at least this big are written straight to disk when the browser
// supports the File System Access API (Chrome, Edge): constant memory
const STREAM_TO_DISK_MIN = 256 * 1024 * 1024;

export function TransferProvider({ children }) {
  const run = useApi();
  const toast = useToast();
  const { tf, lang } = useI18n();
  const [items, setItems] = useState([]);
  const seq = useRef(0);
  const controllers = useRef(new Map());

  const track = useCallback(
    (item) => {
      const id = ++seq.current;
      const ctrl = new AbortController();
      controllers.current.set(id, ctrl);
      const now = Date.now();
      setItems((all) => [{ id, state: "running", done: 0, total: 0, speed: 0, startedAt: now, ...item }, ...all]);
      const update = (patch) => setItems((all) => all.map((x) => (x.id === id ? { ...x, ...patch } : x)));
      const finish = (patch) => {
        controllers.current.delete(id);
        update({ ...patch, speed: 0, endedAt: Date.now() });
      };
      const fail = (e) =>
        isAbort(e) ? finish({ state: "canceled" }) : finish({ state: "error", error: errorText(e, lang) });
      // progress re-renders at most ~4 times per second; speed is smoothed
      let lastT = now;
      let lastDone = 0;
      let speed = 0;
      const progress = (done, total) => {
        const t = Date.now();
        if (t - lastT < 250 && done < total) return;
        const inst = ((done - lastDone) * 1000) / Math.max(1, t - lastT);
        speed = speed ? speed * 0.7 + inst * 0.3 : inst;
        lastT = t;
        lastDone = done;
        update({ done, total: total || done, speed });
      };
      return { progress, finish, fail, signal: ctrl.signal };
    },
    [lang]
  );

  const openSaveSink = async (name, size) => {
    if (size < STREAM_TO_DISK_MIN || typeof window.showSaveFilePicker !== "function") return null;
    const handle = await window.showSaveFilePicker({ suggestedName: name });
    return handle.createWritable();
  };

  // entry: a listing entry (file or folder). Folders download as zip.
  const download = useCallback(
    async (entry) => {
      const name = entry.is_dir ? `${entry.name}.zip` : entry.name;
      let sink = null;
      try {
        sink = entry.is_dir ? null : await openSaveSink(name, entry.size);
      } catch (e) {
        if (isAbort(e)) return; // the user closed the save dialog
      }
      const t = track({ kind: "download", name, path: entry.path, total: entry.is_dir ? 0 : entry.size });
      try {
        const blob = await run((c) => c.download(entry.path, t.progress, t.signal, sink));
        if (sink) {
          await sink.close();
          t.finish({ state: "done", done: entry.size, total: entry.size });
        } else {
          t.finish({ state: "done", done: blob.size, total: blob.size });
          saveBlob(blob, name);
        }
      } catch (e) {
        if (sink) sink.abort().catch(() => {});
        t.fail(e);
      }
    },
    [run, track]
  );

  // Several paths as one zip built on the fly.
  const downloadZip = useCallback(
    async (paths, name) => {
      const t = track({ kind: "download", name, path: paths.join(", "), total: 0 });
      try {
        const blob = await run((c) => c.download({ paths }, (done) => t.progress(done, 0), t.signal));
        t.finish({ state: "done", done: blob.size, total: blob.size });
        saveBlob(blob, name);
      } catch (e) {
        t.fail(e);
      }
    },
    [run, track]
  );

  // Uploads files into dir, one after the other.
  const upload = useCallback(
    async (dir, files) => {
      let ok = 0;
      for (const file of files) {
        const t = track({ kind: "upload", name: file.name, path: dir ? `${dir}/${file.name}` : file.name, total: file.size });
        try {
          await run((c) => c.upload(dir, file, t.progress, t.signal));
          t.finish({ state: "done", done: file.size, total: file.size });
          ok++;
          notifyChanged();
        } catch (e) {
          t.fail(e);
          if (e.status === 401) break;
        }
      }
      if (ok === 1 && files.length === 1) toast.success(tf("toast.uploaded", { name: baseName(files[0].name) }));
      return ok;
    },
    [run, track, toast, tf]
  );

  const cancel = useCallback((id) => controllers.current.get(id)?.abort(), []);
  const cancelAll = useCallback(() => controllers.current.forEach((c) => c.abort()), []);
  const clearFinished = useCallback(() => setItems((all) => all.filter((x) => x.state === "running")), []);

  const summary = useMemo(() => {
    const running = items.filter((x) => x.state === "running");
    const total = running.reduce((a, x) => a + (x.total || 0), 0);
    const done = running.reduce((a, x) => a + Math.min(x.done || 0, x.total || x.done || 0), 0);
    return {
      running: running.length,
      // unknown sizes (zip streams) count as indeterminate
      progress: total > 0 ? done / total : null,
      speed: running.reduce((a, x) => a + (x.speed || 0), 0),
      failed: items.filter((x) => x.state === "error").length,
    };
  }, [items]);

  // Reloading or closing the page cancels running transfers: let the browser
  // ask first (only the browser's own dialog is allowed here).
  useEffect(() => {
    if (!summary.running) return undefined;
    const onBeforeUnload = (e) => {
      e.preventDefault();
      e.returnValue = "";
      return "";
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [summary.running]);

  const value = { items, summary, download, downloadZip, upload, cancel, cancelAll, clearFinished };
  return <TransferContext.Provider value={value}>{children}</TransferContext.Provider>;
}

export function useTransfers() {
  const ctx = useContext(TransferContext);
  if (!ctx) throw new Error("useTransfers must be used within TransferProvider");
  return ctx;
}

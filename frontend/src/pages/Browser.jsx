import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowDown,
  ArrowUp,
  ChevronRight,
  Eye,
  ListFilter,
  Copy,
  Download,
  FileArchive,
  FilePlus2,
  FolderInput,
  FolderPlus,
  Home,
  KeyRound,
  Link2,
  Loader2,
  PackageOpen,
  Pencil,
  RefreshCw,
  Search,
  SquarePen,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useApi, useAuth } from "contexts/AuthContext";
import { useTree } from "contexts/TreeContext";
import { useTransfers } from "contexts/TransferContext";
import { useDialog } from "contexts/DialogContext";
import { useToast } from "contexts/ToastContext";
import { useI18n } from "i18n";
import { Button } from "components/ui/button";
import { Input } from "components/ui/input";
import { Checkbox } from "components/ui/checkbox";
import { ActionMenu } from "components/ui/menu";
import FileIcon from "components/files/FileIcon";
import FolderPicker from "components/files/FolderPicker";
import RequestUploadDialog from "components/files/RequestUploadDialog";
import { canDrop, getDragPaths, isInternalDrag, setDragPaths, useTransfer } from "lib/useTransfer";
import { errorText } from "lib/errors";
import {
  baseName,
  displayName,
  cn,
  humanSize,
  isArchive,
  isEditable,
  isPreviewable,
  joinPath,
  previewKind,
  sortEntries,
  typeGroup,
  TYPE_FILTERS,
  parentPath,
  pathToRoute,
  routeToPath,
} from "lib/utils";

const validName = (n) => !!n && n !== "." && n !== ".." && !/[\\/\0]/.test(n);

function IconButton({ title, onClick, children, danger }) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cn(
        "rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted",
        danger ? "hover:text-red-500" : "hover:text-foreground"
      )}
    >
      {children}
    </button>
  );
}

function Breadcrumb({ path }) {
  const { t } = useI18n();
  const parts = path ? path.split("/") : [];
  return (
    <nav className="flex w-full min-w-0 items-center gap-1 overflow-x-auto text-sm scrollbar-thin xl:w-auto xl:flex-1">
      <Link to="/files" className="flex items-center gap-1 rounded px-1.5 py-1 text-muted-foreground hover:bg-muted hover:text-foreground">
        <Home className="h-4 w-4" />
        <span className="hidden sm:inline">{t("browser.root")}</span>
      </Link>
      {parts.map((p, i) => {
        const sub = parts.slice(0, i + 1).join("/");
        const last = i === parts.length - 1;
        return (
          <span key={sub} className="flex items-center gap-1">
            <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
            {last ? (
              <span className="whitespace-nowrap rounded px-1.5 py-1 font-medium">{p}</span>
            ) : (
              <Link
                to={pathToRoute("/files", sub)}
                className="whitespace-nowrap rounded px-1.5 py-1 text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                {p}
              </Link>
            )}
          </span>
        );
      })}
    </nav>
  );
}

export default function Browser() {
  const params = useParams();
  const path = routeToPath(params["*"]);
  const navigate = useNavigate();
  const run = useApi();
  const { user } = useAuth();
  const dialog = useDialog();
  const toast = useToast();
  const { t, tf, lang } = useI18n();
  const tree = useTree();

  const [listing, setListing] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState({});
  const [query, setQuery] = useState("");
  const [results, setResults] = useState(null); // search results
  const [searching, setSearching] = useState(false);
  const [dragging, setDragging] = useState(false);
  const transfers = useTransfers();
  const fileInput = useRef(null);
  const transfer = useTransfer();
  const [picker, setPicker] = useState(null); // {paths, action}
  const [requestOpen, setRequestOpen] = useState(false);
  const [dropTarget, setDropTarget] = useState(null);
  const [view, setView] = useState(() => {
    try {
      return { sort: "name", dir: "asc", filter: "all", ...JSON.parse(localStorage.getItem("nullops.view") || "{}") };
    } catch {
      return { sort: "name", dir: "asc", filter: "all" };
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem("nullops.view", JSON.stringify(view));
    } catch {
      /* ignore */
    }
  }, [view]);
  const sortBy = (key) =>
    setView((v) => ({ ...v, sort: key, dir: v.sort === key && v.dir === "asc" ? "desc" : "asc" }));
  // Columns follow the space actually available (the folder panel takes part
  // of the window), not the viewport width.
  const listRef = useRef(null);
  const [listWidth, setListWidth] = useState(1200);
  useEffect(() => {
    const el = listRef.current;
    if (!el || typeof ResizeObserver === "undefined") return undefined;
    const ro = new ResizeObserver(([e]) => setListWidth(e.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const showSize = listWidth >= 520;
  const showDate = listWidth >= 700;
  const showPerms = listWidth >= 860;
  const dragDepth = useRef(0);

  const readonly = !!user?.readonly;
  const err = useCallback((e) => errorText(e, lang), [lang]);

  // Only the latest request may fill the screen: a slow answer for a folder
  // the user already left is dropped.
  const loadSeq = useRef(0);
  const load = useCallback(async () => {
    const mine = ++loadSeq.current;
    setLoading(true);
    setError("");
    try {
      const res = await run((c) => c.call("list", { path }));
      if (mine !== loadSeq.current) return;
      setListing(res);
      tree.update(path, res.entries);
    } catch (e) {
      if (mine !== loadSeq.current) return;
      if (e.status !== 401) setError(err(e));
      setListing(null);
    } finally {
      if (mine === loadSeq.current) setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, path, err]);

  // Opening a folder: clear the previous folder's items right away, show the
  // loading state, then fill the list when the server answers.
  useEffect(() => {
    setListing(null);
    setSelected({});
    setQuery("");
    setResults(null);
    load();
  }, [load]);

  useEffect(() => {
    const onChanged = () => load();
    window.addEventListener("nullops:changed", onChanged);
    return () => window.removeEventListener("nullops:changed", onChanged);
  }, [load]);

  const allEntries = useMemo(() => (results ? results : listing?.entries || []), [results, listing]);
  const entries = useMemo(() => {
    const filtered = view.filter === "all" ? allEntries : allEntries.filter((e) => typeGroup(e) === view.filter);
    return sortEntries(filtered, view.sort, view.dir);
  }, [allEntries, view]);
  const selectedPaths = Object.keys(selected).filter((k) => selected[k]);
  const allSelected = entries.length > 0 && selectedPaths.length === entries.length;

  // ---- helpers -----------------------------------------------------------
  const act = async (fn, okMsg) => {
    try {
      await run(fn);
      if (okMsg) toast.success(okMsg);
      setSelected({});
      if (results) setResults(null);
      await load();
    } catch (e) {
      if (e.status !== 401) toast.error(err(e));
    }
  };

  // Used as onSubmit of prompts: errors keep the dialog open.
  const submit = (fn) => async (value) => {
    await run((c) => fn(c, value)).catch((e) => {
      throw new Error(err(e));
    });
  };

  // Folders open; files are sniffed by the server (MIME of the first 4096
  // bytes): text/* goes to the viewer/editor, anything else is downloaded.
  // The extension list is only the fallback when the probe fails.
  const [probing, setProbing] = useState(null);
  const open = async (entry) => {
    if (entry.is_dir) {
      navigate(pathToRoute("/files", entry.path));
      return;
    }
    if (probing) return;
    setProbing(entry.path);
    let kind;
    try {
      const p = await run((c) => c.call("probe", { path: entry.path }));
      kind = previewKind(p.mime, p.is_text);
    } catch (e) {
      if (e.status === 401) return;
      kind = isEditable(entry.name) ? "text" : "other";
    } finally {
      setProbing(null);
    }
    if (kind === "text") navigate(pathToRoute("/edit", entry.path));
    else if (kind === "image" || kind === "pdf") navigate(pathToRoute("/view", entry.path));
    else download(entry);
  };

  // ---- transfers (global: they keep running while browsing, see /transfers) --
  const download = (entry) => transfers.download(entry);

  const downloadSelection = (paths) => {
    if (paths.length === 1) {
      const entry = entries.find((x) => x.path === paths[0]);
      if (entry) return transfers.download(entry);
    }
    const stamp = new Date().toISOString().slice(0, 19).replace(/\D/g, "").replace(/^(\d{8})/, "$1_");
    return transfers.downloadZip(paths, `${baseName(path) || "files"}_${stamp}.zip`);
  };

  const uploadFiles = (files) => {
    if (readonly || !files.length) return;
    transfers.upload(path, files);
  };

  // ---- actions -------------------------------------------------------------
  const newFolder = () =>
    dialog.prompt({
      title: t("dialog.newFolder"),
      label: t("dialog.name"),
      confirmLabel: t("common.create"),
      validate: (v) => (validName(v.trim()) ? "" : t("dialog.invalidName")),
      onSubmit: submit((c, v) => c.call("mkdir", { path, name: v.trim() })),
    }).then((v) => v !== null && (toast.success(tf("toast.created", { name: v.trim() })), load()));

  const newFile = () =>
    dialog.prompt({
      title: t("dialog.newFile"),
      label: t("dialog.name"),
      confirmLabel: t("common.create"),
      validate: (v) => (validName(v.trim()) ? "" : t("dialog.invalidName")),
      onSubmit: submit((c, v) => c.call("touch", { path, name: v.trim() })),
    }).then((v) => {
      if (v === null) return;
      toast.success(tf("toast.created", { name: v.trim() }));
      // a new file is empty, i.e. text: open it in the editor
      navigate(pathToRoute("/edit", joinPath(path, v.trim())));
    });

  const rename = (entry) =>
    dialog.prompt({
      title: tf("dialog.rename", { name: entry.name }),
      label: t("dialog.name"),
      defaultValue: entry.name,
      selectStem: !entry.is_dir,
      validate: (v) => (validName(v.trim()) ? "" : t("dialog.invalidName")),
      onSubmit: submit((c, v) => c.call("rename", { path: entry.path, name: v.trim() })),
    }).then((v) => v !== null && (toast.success(tf("toast.renamed", { name: v.trim() })), load()));

  const copyMove = (paths, action) => setPicker({ paths, action });

  const chmod = (entry) =>
    dialog.prompt({
      title: tf("dialog.chmod", { name: entry.name }),
      hint: t("dialog.chmodHint"),
      defaultValue: (entry.mode & 0o777).toString(8).padStart(3, "0"),
      validate: (v) => (/^[0-7]{3}$/.test(v.trim()) ? "" : t("dialog.invalidMode")),
      onSubmit: submit((c, v) => c.call("chmod", { path: entry.path, mode: v.trim() })),
    }).then((v) => v !== null && (toast.success(t("toast.chmod")), load()));

  const extract = (entry) =>
    dialog.prompt({
      title: tf("dialog.extractTitle", { name: entry.name }),
      variant: "info",
      noInput: true, // the prompt is used only for its Switch row
      toggleLabel: t("dialog.extractSub"),
      toggleDescription: t("dialog.extractSubDesc"),
      toggleDefault: true,
      confirmLabel: t("action.extract"),
      onSubmit: submit((c, v) => c.call("extract", { path: entry.path, to_subfolder: v.toggle })),
    }).then((v) => {
      if (v === null) return;
      toast.success(t("toast.extracted"));
      load();
    });

  const archive = (paths, kind) => {
    const stamp = new Date().toISOString().slice(0, 19).replace(/\D/g, "").replace(/^(\d{8})/, "$1_");
    const ext = kind === "zip" ? ".zip" : ".tar.gz";
    const def = paths.length === 1 ? baseName(paths[0]) + ext : `archive_${stamp}${ext}`;
    return dialog.prompt({
      title: tf("dialog.archiveTitle", { type: kind === "zip" ? "ZIP" : "TAR.GZ" }),
      label: t("dialog.archiveName"),
      defaultValue: def,
      selectStem: true,
      validate: (v) => (validName(v.trim()) ? "" : t("dialog.invalidName")),
      onSubmit: submit((c, v) => c.call(kind, { paths, dest: path, name: v.trim() })),
    }).then((v) => {
      if (v === null) return;
      toast.success(tf("toast.archived", { name: v.trim() }));
      setSelected({});
      load();
    });
  };

  const remove = (paths) => {
    const names = paths.length <= 3 ? paths.map((p) => `“${baseName(p)}”`).join(", ") : `${paths.length}`;
    return dialog.confirm({
      title: tf("dialog.deleteTitle", { n: paths.length }),
      description: tf("dialog.deleteDesc", { names }),
      variant: "danger",
      confirmLabel: t("action.delete"),
      onConfirm: () =>
        run((c) => c.call("delete", { paths })).catch((e) => {
          throw new Error(err(e));
        }),
    }).then((ok) => {
      if (!ok) return;
      toast.success(tf("toast.deleted", { n: paths.length }));
      setSelected({});
      load();
    });
  };

  // ---- search ----------------------------------------------------------------
  const doSearch = async (e) => {
    e.preventDefault();
    const q = query.trim();
    if (!q) {
      setResults(null);
      return;
    }
    setSearching(true);
    setSelected({});
    try {
      const r = await run((c) => c.call("search", { path, query: q }));
      setResults(r.results || []);
    } catch (e2) {
      if (e2.status !== 401) toast.error(err(e2));
    } finally {
      setSearching(false);
    }
  };

  // ---- drag & drop -----------------------------------------------------------
  const dropHandlers = readonly
    ? {}
    : {
        onDragEnter: (e) => {
          if (isInternalDrag(e) || !e.dataTransfer?.types?.includes("Files")) return;
          e.preventDefault();
          dragDepth.current++;
          setDragging(true);
        },
        onDragOver: (e) => {
          if (!isInternalDrag(e) && e.dataTransfer?.types?.includes("Files")) e.preventDefault();
        },
        onDragLeave: () => {
          dragDepth.current = Math.max(0, dragDepth.current - 1);
          if (!dragDepth.current) setDragging(false);
        },
        onDrop: (e) => {
          if (isInternalDrag(e)) return;
          e.preventDefault();
          dragDepth.current = 0;
          setDragging(false);
          uploadFiles(Array.from(e.dataTransfer.files || []));
        },
      };

  const toggleAll = (v) => setSelected(v ? Object.fromEntries(entries.map((x) => [x.path, true])) : {});

  // ---- render ----------------------------------------------------------------
  return (
    <div className="relative flex min-h-0 flex-1 flex-col" {...dropHandlers}>
      {/* toolbar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <Breadcrumb path={path} />
        <form onSubmit={doSearch} className="relative w-full sm:w-64">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("browser.search")}
            className="h-9 pl-8"
          />
        </form>
        <label className="inline-flex h-9 items-center gap-1.5 rounded-md border border-input px-2 text-sm text-muted-foreground">
          <ListFilter className="h-4 w-4" />
          <span className="sr-only">{t("browser.filter")}</span>
          <select
            value={view.filter}
            onChange={(e) => setView((v) => ({ ...v, filter: e.target.value }))}
            className="bg-transparent text-foreground outline-none"
          >
            {TYPE_FILTERS.map((f) => (
              <option key={f} value={f} className="bg-card">
                {t("filter." + f)}
              </option>
            ))}
          </select>
        </label>
        <Button variant="outline" size="sm" onClick={load} title={t("browser.refresh")}>
          <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
        </Button>
        {!readonly && (
          <>
            <Button variant="outline" size="sm" onClick={newFolder}>
              <FolderPlus className="h-4 w-4" /> {t("browser.newFolder")}
            </Button>
            <Button variant="outline" size="sm" onClick={newFile}>
              <FilePlus2 className="h-4 w-4" /> {t("browser.newFile")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setRequestOpen(true)} title={t("reqUpload.button")}>
              <Link2 className="h-4 w-4" /> {t("reqUpload.button")}
            </Button>
            <Button size="sm" onClick={() => fileInput.current?.click()}>
              <Upload className="h-4 w-4" /> {t("browser.upload")}
            </Button>
            <input
              ref={fileInput}
              type="file"
              multiple
              className="hidden"
              onChange={(e) => {
                uploadFiles(Array.from(e.target.files || []));
                e.target.value = "";
              }}
            />
          </>
        )}
      </div>

      {/* batch bar */}
      {selectedPaths.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-4 py-2 text-sm">
          <span className="font-medium">{tf("browser.selected", { n: selectedPaths.length })}</span>
          <div className="ml-auto flex flex-wrap gap-2">
            <Button variant="outline" size="sm" onClick={() => downloadSelection(selectedPaths)}>
              <Download className="h-4 w-4" /> {t("action.downloadZip")}
            </Button>
            {!readonly && (
              <>
                <Button variant="outline" size="sm" onClick={() => archive(selectedPaths, "zip")}>
                  <FileArchive className="h-4 w-4" /> {t("action.zip")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => archive(selectedPaths, "tar")}>
                  <FileArchive className="h-4 w-4" /> {t("action.tar")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => copyMove(selectedPaths, "copy")}>
                  <Copy className="h-4 w-4" /> {t("action.copy")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => copyMove(selectedPaths, "move")}>
                  <FolderInput className="h-4 w-4" /> {t("action.move")}
                </Button>
                <Button variant="destructive" size="sm" onClick={() => remove(selectedPaths)}>
                  <Trash2 className="h-4 w-4" /> {t("action.delete")}
                </Button>
              </>
            )}
            <Button variant="ghost" size="sm" onClick={() => setSelected({})} title={t("common.close")}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}

      {results && (
        <div className="flex items-center gap-2 border-b border-border px-4 py-2 text-sm text-muted-foreground">
          <Search className="h-4 w-4" />
          {tf("browser.searchResults", { n: results.length, q: query.trim() })}
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => {
              setResults(null);
              setQuery("");
            }}
          >
            <X className="h-4 w-4" /> {t("browser.clearSearch")}
          </Button>
        </div>
      )}

      {/* listing */}
      <div className="relative h-0.5 shrink-0 overflow-hidden">
        {loading && <div className="absolute inset-y-0 w-1/3 animate-loading-bar bg-primary" />}
      </div>
      <div ref={listRef} className="min-h-0 flex-1 overflow-auto scrollbar-thin">
        {error ? (
          <div className="p-8 text-center text-sm text-red-500">{error}</div>
        ) : loading && !listing ? (
          <div className="flex flex-col items-center justify-center gap-3 p-16 text-sm text-muted-foreground">
            <Loader2 className="h-7 w-7 animate-spin text-primary" />
            {t("browser.loading")}
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead className="sticky top-0 z-10 bg-card text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr className="border-b border-border">
                <th className="w-10 px-4 py-2">
                  <Checkbox
                    checked={allSelected}
                    indeterminate={!allSelected && selectedPaths.length > 0}
                    onChange={toggleAll}
                    ariaLabel={t("browser.selectAll")}
                  />
                </th>
                <th className="px-2 py-2 font-medium">
                    <button type="button" onClick={() => sortBy("name")} className="inline-flex items-center gap-1 uppercase hover:text-foreground">
                      {t("browser.name")}
                      {view.sort === "name" && (view.dir === "asc" ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />)}
                    </button>
                  </th>
                {showSize && (<th className="w-28 px-2 py-2 text-right font-medium">
                    <button type="button" onClick={() => sortBy("size")} className="inline-flex items-center gap-1 uppercase hover:text-foreground">
                      {t("browser.size")}
                      {view.sort === "size" && (view.dir === "asc" ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />)}
                    </button>
                  </th>)}
                {showDate && (<th className="w-40 px-2 py-2 font-medium">
                    <button type="button" onClick={() => sortBy("mtime")} className="inline-flex items-center gap-1 uppercase hover:text-foreground">
                      {t("browser.modified")}
                      {view.sort === "mtime" && (view.dir === "asc" ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />)}
                    </button>
                  </th>)}
                {showPerms && <th className="w-32 px-2 py-2 font-medium">{t("browser.perms")}</th>}
                <th className="w-px whitespace-nowrap px-4 py-2 text-right font-medium">{t("browser.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {path && !results && (
                <tr
                  className="cursor-pointer border-b border-border/50 hover:bg-muted/40"
                  onClick={() => navigate(pathToRoute("/files", parentPath(path)))}
                >
                  <td className="px-4 py-2" />
                  <td className="px-2 py-2 text-muted-foreground" colSpan={2 + showSize + showDate + showPerms}>
                    <span className="inline-flex items-center gap-2">
                      <ArrowUp className="h-4 w-4" /> ..
                    </span>
                  </td>
                </tr>
              )}
              {entries.map((e) => (
                <tr
                  key={e.path}
                  className={cn(
                    "group cursor-pointer border-b border-border/50 hover:bg-muted/40",
                    selected[e.path] && "bg-primary/10 hover:bg-primary/15",
                    dropTarget === e.path && "bg-primary/20 outline outline-2 -outline-offset-2 outline-primary"
                  )}
                  onClick={() => open(e)}
                  draggable={!readonly}
                  onDragStart={(ev) => {
                    const paths = selected[e.path] ? selectedPaths : [e.path];
                    setDragPaths(ev, paths);
                  }}
                  onDragOver={(ev) => {
                    if (!e.is_dir || readonly || !isInternalDrag(ev)) return;
                    ev.preventDefault();
                    ev.stopPropagation();
                    ev.dataTransfer.dropEffect = ev.ctrlKey || ev.altKey ? "copy" : "move";
                    if (dropTarget !== e.path) setDropTarget(e.path);
                  }}
                  onDragLeave={() => dropTarget === e.path && setDropTarget(null)}
                  onDrop={(ev) => {
                    if (!e.is_dir || readonly || !isInternalDrag(ev)) return;
                    ev.preventDefault();
                    ev.stopPropagation();
                    setDropTarget(null);
                    const paths = getDragPaths(ev);
                    if (paths && canDrop(paths, e.path)) {
                      transfer(paths, e.path, ev.ctrlKey || ev.altKey).then((ok) => ok && setSelected({}));
                    }
                  }}
                  title={readonly ? undefined : t("dnd.hint")}
                >
                  <td className="px-4 py-2">
                    <Checkbox
                      checked={!!selected[e.path]}
                      onChange={(v) => setSelected((s) => ({ ...s, [e.path]: v }))}
                      ariaLabel={e.name}
                    />
                  </td>
                  <td className="max-w-0 px-2 py-2">
                    <div className="flex items-center gap-2">
                      {probing === e.path ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      ) : (
                        <FileIcon entry={e} />
                      )}
                      <span className="truncate" title={e.path}>
                        {displayName(results ? e.path : e.name)}
                      </span>
                      {e.is_link && <span className="text-xs text-muted-foreground">↪</span>}
                    </div>
                  </td>
                  {showSize && (
                    <td className="whitespace-nowrap px-2 py-2 text-right tabular-nums text-muted-foreground">
                      {e.is_dir ? "—" : humanSize(e.size)}
                    </td>
                  )}
                  {showDate && (
                    <td className="whitespace-nowrap px-2 py-2 tabular-nums text-muted-foreground">{e.mtime_str}</td>
                  )}
                  {showPerms && (
                    <td className="whitespace-nowrap px-2 py-2 font-mono text-xs text-muted-foreground">{e.perms}</td>
                  )}
                  <td className="px-4 py-1.5">
                    <div className="flex justify-end gap-0.5 opacity-70 group-hover:opacity-100">
                      {!e.is_dir && isPreviewable(e.name) && (
                        <IconButton title={t("action.view")} onClick={() => navigate(pathToRoute("/view", e.path))}>
                          <Eye className="h-4 w-4" />
                        </IconButton>
                      )}
                      <IconButton
                        title={e.is_dir ? t("action.downloadZip") : t("action.download")}
                        onClick={() => download(e)}
                      >
                        <Download className="h-4 w-4" />
                      </IconButton>
                      {!e.is_dir && isEditable(e.name) && !readonly && (
                        <IconButton title={t("action.edit")} onClick={() => navigate(pathToRoute("/edit", e.path))}>
                          <SquarePen className="h-4 w-4" />
                        </IconButton>
                      )}
                      <ActionMenu
                        title={t("browser.actions")}
                        items={[
                          { label: t("action.extract"), icon: PackageOpen, onClick: () => extract(e), hidden: readonly || e.is_dir || !isArchive(e.name) },
                          { label: t("action.rename"), icon: Pencil, onClick: () => rename(e), hidden: readonly },
                          { label: t("action.copy"), icon: Copy, onClick: () => copyMove([e.path], "copy"), hidden: readonly },
                          { label: t("action.move"), icon: FolderInput, onClick: () => copyMove([e.path], "move"), hidden: readonly },
                          { label: t("action.chmod"), icon: KeyRound, onClick: () => chmod(e), hidden: readonly },
                          { label: t("action.delete"), icon: Trash2, onClick: () => remove([e.path]), danger: true, hidden: readonly },
                        ]}
                      />
                    </div>
                  </td>
                </tr>
              ))}
              {!entries.length && !loading && (
                <tr>
                  <td colSpan={3 + showSize + showDate + showPerms} className="p-12 text-center text-muted-foreground">
                    {results ? tf("browser.noResults", { q: query.trim() }) : t("browser.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {listing && !results && (
        <div className="border-t border-border px-4 py-1.5 text-xs text-muted-foreground">
          {tf("browser.summary", { d: listing.dir_count, f: listing.file_count, s: humanSize(listing.total_size) })}
          {view.filter !== "all" && ` · ${tf("browser.filtered", { n: entries.length, total: allEntries.length })}`}
          {searching && <Loader2 className="ml-2 inline h-3 w-3 animate-spin" />}
        </div>
      )}

      {dragging && (
        <div className="pointer-events-none absolute inset-2 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-primary/10">
          <div className="flex items-center gap-2 rounded-lg bg-card px-4 py-3 text-sm font-medium shadow">
            <Upload className="h-4 w-4 text-primary" /> {t("browser.dropHere")}
          </div>
        </div>
      )}

      <RequestUploadDialog open={requestOpen} path={path} onClose={() => setRequestOpen(false)} />

      <FolderPicker
        open={!!picker}
        title={picker ? tf("picker.title", { action: t(picker.action === "copy" ? "action.copy" : "action.move"), n: picker.paths.length }) : ""}
        initial={path}
        exclude={picker?.action === "move" ? picker.paths : []}
        confirmLabel={picker ? t(picker.action === "copy" ? "action.copy" : "action.move") : ""}
        onCancel={() => setPicker(null)}
        onPick={async (dest) => {
          const ok = await transfer(picker.paths, dest, picker.action === "copy");
          if (ok) {
            setPicker(null);
            setSelected({});
          }
        }}
      />

    </div>
  );
}

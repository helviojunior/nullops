import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, Folder, FolderOpen, HardDrive, Loader2, PanelLeftClose, AlertTriangle, StickyNote } from "lucide-react";
import PadList from "components/pads/PadList";
import { useTree } from "contexts/TreeContext";
import { useI18n } from "i18n";
import { cn, joinPath, pathToRoute } from "lib/utils";
import { useAuth } from "contexts/AuthContext";
import { canDrop, getDragPaths, isInternalDrag, useTransfer } from "lib/useTransfer";

export const TREE_MIN = 180;
export const TREE_MAX = 600;
export const TREE_DEFAULT = 260;

function TreeNode({ path, name, depth, current }) {
  const { nodes, expanded, toggle } = useTree();
  const { user } = useAuth();
  const transfer = useTransfer();
  const [over, setOver] = useState(false);
  const canWrite = !user?.readonly;
  const node = nodes[path];
  const open = expanded.has(path);
  const active = path === current;
  const hasKids = !node || node.dirs === null || node.dirs.length > 0;
  const Icon = path === "" ? HardDrive : open ? FolderOpen : Folder;

  return (
    <li>
      <div
        className={cn(
          "group flex items-center gap-1 rounded-md pr-2 text-sm",
          active ? "bg-primary/15 text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
          over && "bg-primary/25 text-foreground outline outline-2 -outline-offset-2 outline-primary"
        )}
        style={{ paddingLeft: depth * 14 + 4 }}
        onDragOver={(e) => {
          if (!canWrite || !isInternalDrag(e)) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = e.ctrlKey || e.altKey ? "copy" : "move";
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          if (!canWrite || !isInternalDrag(e)) return;
          e.preventDefault();
          setOver(false);
          const paths = getDragPaths(e);
          if (paths && canDrop(paths, path)) transfer(paths, path, e.ctrlKey || e.altKey);
        }}
      >
        <button
          type="button"
          onClick={() => toggle(path)}
          className={cn("flex h-6 w-5 shrink-0 items-center justify-center rounded", !hasKids && "invisible")}
          aria-label={name}
          aria-expanded={open}
          tabIndex={hasKids ? 0 : -1}
        >
          {node?.loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-90")} />
          )}
        </button>
        <Link
          to={pathToRoute("/files", path)}
          className="flex min-w-0 flex-1 items-center gap-1.5 py-1"
          title={path ? "/" + path : "/"}
          aria-current={active ? "true" : undefined}
        >
          <Icon
            className={cn("h-4 w-4 shrink-0", path === "" ? "text-primary" : "text-amber-400")}
            fill={path === "" ? "none" : "currentColor"}
            fillOpacity={0.25}
          />
          <span className={cn("truncate", active && "font-medium")}>{name}</span>
        </Link>
        {node?.error && <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-red-500" />}
      </div>
      {open && node?.dirs && node.dirs.length > 0 && (
        <ul>
          {node.dirs.map((d) => (
            <TreeNode key={d} path={joinPath(path, d)} name={d} depth={depth + 1} current={current} />
          ))}
        </ul>
      )}
    </li>
  );
}

/**
 * Left side panel with the folder hierarchy. Resizable by dragging its right
 * edge (double-click resets the width) and collapsible.
 */
export default function FolderTree({ current, currentPad, tab, setTab, width, setWidth, onCollapse }) {
  const { t } = useI18n();
  const { reveal } = useTree();
  const [dragging, setDragging] = useState(false);
  const startRef = useRef(null);
  const listRef = useRef(null);

  useEffect(() => {
    if (tab !== "pads" && current != null) reveal(current);
  }, [current, reveal, tab]);

  // keep the active folder in view when the user navigates
  useEffect(() => {
    const id = setTimeout(() => {
      const el = listRef.current?.querySelector('[aria-current="true"]');
      if (el) el.scrollIntoView({ block: "nearest" });
    }, 150);
    return () => clearTimeout(id);
  }, [current]);

  const onPointerDown = useCallback(
    (e) => {
      e.preventDefault();
      startRef.current = { x: e.clientX, w: width };
      setDragging(true);
    },
    [width]
  );

  useEffect(() => {
    if (!dragging) return undefined;
    const move = (e) => {
      const s = startRef.current;
      setWidth(Math.min(TREE_MAX, Math.max(TREE_MIN, s.w + e.clientX - s.x)));
    };
    const up = () => setDragging(false);
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [dragging, setWidth]);

  const onKeyDown = (e) => {
    if (e.key === "ArrowLeft") setWidth(Math.max(TREE_MIN, width - 20));
    if (e.key === "ArrowRight") setWidth(Math.min(TREE_MAX, width + 20));
  };

  return (
    <aside
      className="relative flex shrink-0 flex-col border-r border-border bg-card"
      style={{ width }}
      aria-label={t("tree.title")}
    >
      <div className="flex h-10 shrink-0 items-stretch border-b border-border pl-1 pr-2 text-xs font-medium uppercase tracking-wide">
        <div role="tablist" className="flex min-w-0 flex-1 items-stretch">
          {[
            ["folders", t("tree.title"), Folder],
            ["pads", t("pad.tab"), StickyNote],
          ].map(([id, label, Icon]) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={tab === id}
              onClick={() => setTab(id)}
              className={cn(
                "inline-flex min-w-0 items-center gap-1.5 border-b-2 px-2.5 transition-colors",
                tab === id
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              <Icon className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate">{label}</span>
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={onCollapse}
          className="my-auto rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          title={t("tree.hide")}
          aria-label={t("tree.hide")}
        >
          <PanelLeftClose className="h-4 w-4" />
        </button>
      </div>
      {tab === "pads" ? (
        <PadList current={currentPad} />
      ) : (
        <ul ref={listRef} className="min-h-0 flex-1 overflow-auto p-1.5 scrollbar-thin">
          <TreeNode path="" name={t("browser.root")} depth={0} current={current} />
        </ul>
      )}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={t("tree.resize")}
        title={t("tree.resize")}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onDoubleClick={() => setWidth(TREE_DEFAULT)}
        onKeyDown={onKeyDown}
        className={cn(
          "absolute -right-1 top-0 z-20 h-full w-2 cursor-col-resize transition-colors hover:bg-primary/40 focus-visible:bg-primary/40 focus-visible:outline-none",
          dragging && "bg-primary/60"
        )}
      />
    </aside>
  );
}

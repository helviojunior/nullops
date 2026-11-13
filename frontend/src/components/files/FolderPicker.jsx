import { useCallback, useEffect, useState } from "react";
import { ChevronRight, Folder, FolderOpen, HardDrive, Loader2 } from "lucide-react";
import { Modal } from "components/ui/modal";
import { Button } from "components/ui/button";
import { useApi } from "contexts/AuthContext";
import { useI18n } from "i18n";
import { cn, joinPath } from "lib/utils";

/**
 * Destination chooser for copy / move: a folder tree (loaded on demand) in
 * the system modal. Folders being moved (and their descendants) are disabled.
 */
export default function FolderPicker({ open, title, initial = "", exclude = [], confirmLabel, onCancel, onPick }) {
  const run = useApi();
  const { t, tf } = useI18n();
  const [nodes, setNodes] = useState({});
  const [expanded, setExpanded] = useState(new Set([""]));
  const [selected, setSelected] = useState(initial);
  const [busy, setBusy] = useState(false);

  const load = useCallback(
    async (path) => {
      setNodes((n) => ({ ...n, [path]: { ...(n[path] || {}), loading: true } }));
      try {
        const r = await run((c) => c.call("list", { path }));
        setNodes((n) => ({ ...n, [path]: { dirs: r.entries.filter((e) => e.is_dir).map((e) => e.name) } }));
      } catch {
        setNodes((n) => ({ ...n, [path]: { dirs: [] } }));
      }
    },
    [run]
  );

  useEffect(() => {
    if (!open) return;
    setSelected(initial);
    setNodes({});
    // expand the chain down to the initial folder
    const parts = initial ? initial.split("/") : [];
    const chain = [""];
    parts.forEach((_, i) => chain.push(parts.slice(0, i + 1).join("/")));
    setExpanded(new Set(chain));
    chain.forEach(load);
  }, [open, initial, load]);

  const blocked = (p) => exclude.some((x) => p === x || p.startsWith(x + "/"));

  const toggle = (p) => {
    setExpanded((ex) => {
      const next = new Set(ex);
      if (next.has(p)) next.delete(p);
      else {
        next.add(p);
        if (!nodes[p]?.dirs) load(p);
      }
      return next;
    });
  };

  const Node = ({ path, name, depth }) => {
    const n = nodes[path];
    const isOpen = expanded.has(path);
    const disabled = blocked(path);
    const Icon = path === "" ? HardDrive : isOpen ? FolderOpen : Folder;
    return (
      <li>
        <div
          className={cn(
            "flex items-center gap-1 rounded-md pr-2 text-sm",
            selected === path ? "bg-primary/15" : "hover:bg-muted",
            disabled && "opacity-40"
          )}
          style={{ paddingLeft: depth * 14 + 4 }}
        >
          <button type="button" onClick={() => toggle(path)} className="flex h-6 w-5 items-center justify-center">
            {n?.loading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", isOpen && "rotate-90")} />
            )}
          </button>
          <button
            type="button"
            disabled={disabled}
            onClick={() => setSelected(path)}
            onDoubleClick={() => toggle(path)}
            className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left"
          >
            <Icon className={cn("h-4 w-4 shrink-0", path === "" ? "text-primary" : "text-amber-400")} />
            <span className="truncate">{name}</span>
          </button>
        </div>
        {isOpen && n?.dirs?.length > 0 && (
          <ul>
            {n.dirs.map((d) => (
              <Node key={d} path={joinPath(path, d)} name={d} depth={depth + 1} />
            ))}
          </ul>
        )}
      </li>
    );
  };

  return (
    <Modal open={open} onClose={busy ? undefined : onCancel} title={title} size="lg"
      footer={
        <>
          <Button variant="outline" onClick={onCancel} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button
            loading={busy}
            disabled={blocked(selected)}
            onClick={async () => {
              setBusy(true);
              try {
                await onPick(selected);
              } finally {
                setBusy(false);
              }
            }}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <p className="mb-2 text-left text-xs text-muted-foreground">{t("picker.choose")}</p>
      <ul className="max-h-80 overflow-auto rounded-lg border border-border p-1.5 text-left scrollbar-thin">
        <Node path="" name={t("browser.root")} depth={0} />
      </ul>
      <p className="mt-2 truncate text-left font-mono text-xs">{tf("picker.selected", { path: "/" + selected })}</p>
    </Modal>
  );
}

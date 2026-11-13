import { useCallback } from "react";
import { useApi } from "contexts/AuthContext";
import { useToast } from "contexts/ToastContext";
import { useTree } from "contexts/TreeContext";
import { useI18n } from "i18n";
import { errorText } from "lib/errors";
import { baseName, parentPath } from "lib/utils";

// Drag-and-drop payload type for file rows.
export const DND_TYPE = "application/x-fmweb-paths";

export function setDragPaths(e, paths) {
  e.dataTransfer.setData(DND_TYPE, JSON.stringify(paths));
  e.dataTransfer.setData("text/plain", paths.join("\n"));
  e.dataTransfer.effectAllowed = "copyMove";
}

export function getDragPaths(e) {
  try {
    const v = e.dataTransfer.getData(DND_TYPE);
    return v ? JSON.parse(v) : null;
  } catch {
    return null;
  }
}

export const isInternalDrag = (e) => !!e.dataTransfer?.types?.includes(DND_TYPE);

// A folder cannot receive itself, its own parent's copy of itself, or a descendant.
export function canDrop(paths, dest) {
  return paths.every((p) => p !== dest && !dest.startsWith(p + "/") && parentPath(p) !== dest);
}

// Notifies listings that the filesystem changed (Browser reloads).
export function notifyChanged() {
  window.dispatchEvent(new CustomEvent("fmweb:changed"));
}

/** useTransfer() -> (paths, dest, copy) => Promise<boolean>: copy/move + refresh. */
export function useTransfer() {
  const run = useApi();
  const toast = useToast();
  const tree = useTree();
  const { tf, lang } = useI18n();
  return useCallback(
    async (paths, dest, copy) => {
      try {
        await run((c) => c.call(copy ? "copy" : "move", { paths, dest }));
        tree.invalidate(dest);
        paths.forEach((p) => tree.invalidate(parentPath(p)));
        toast.success(tf(copy ? "dnd.copied" : "dnd.moved", { name: dest ? baseName(dest) : "/" }));
        notifyChanged();
        return true;
      } catch (err) {
        if (err.status !== 401) toast.error(errorText(err, lang));
        return false;
      }
    },
    [run, toast, tree, tf, lang]
  );
}

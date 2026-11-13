import { createContext, useCallback, useContext, useRef, useState } from "react";
import { useApi } from "contexts/AuthContext";

/**
 * Folder hierarchy shown in the side panel. Children are loaded lazily (only
 * folders), and the Browser feeds every listing it fetches back in, so
 * navigating keeps the tree current without extra requests.
 *
 * nodes[path] = { dirs: string[] | null, loading: bool, error: bool }
 * ("" is the root)
 */
const TreeContext = createContext(null);

export const ancestorsOf = (path) => {
  const parts = path ? path.split("/") : [];
  const out = [""];
  for (let i = 0; i < parts.length; i++) out.push(parts.slice(0, i + 1).join("/"));
  return out;
};

export function TreeProvider({ children }) {
  const run = useApi();
  const [nodes, setNodes] = useState({});
  const [expanded, setExpanded] = useState(() => new Set([""]));
  const inflight = useRef(new Set());
  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const expandedRef = useRef(expanded);
  expandedRef.current = expanded;

  const setNode = (path, patch) =>
    setNodes((all) => ({ ...all, [path]: { dirs: null, loading: false, error: false, ...all[path], ...patch } }));

  const load = useCallback(
    async (path, force = false) => {
      const n = nodesRef.current[path];
      if (!force && n && (n.dirs || n.loading)) return;
      if (inflight.current.has(path)) return;
      inflight.current.add(path);
      setNode(path, { loading: true, error: false });
      try {
        const res = await run((c) => c.call("list", { path }));
        setNode(path, { dirs: res.entries.filter((e) => e.is_dir).map((e) => e.name), loading: false });
      } catch {
        setNode(path, { loading: false, error: true });
      } finally {
        inflight.current.delete(path);
      }
    },
    [run]
  );

  // Called by the Browser with each listing it fetched.
  const update = useCallback((path, entries) => {
    setNode(path, { dirs: entries.filter((e) => e.is_dir).map((e) => e.name), loading: false, error: false });
  }, []);

  // Marks a folder as stale (e.g. destination of a copy/move); reloads it when visible.
  const invalidate = useCallback(
    (path) => {
      setNodes((all) => {
        if (!all[path]) return all;
        const next = { ...all };
        delete next[path];
        return next;
      });
      if (expandedRef.current.has(path)) load(path, true);
    },
    [load]
  );

  const toggle = useCallback(
    (path) => {
      const open = !expandedRef.current.has(path);
      setExpanded((ex) => {
        const next = new Set(ex);
        if (open) next.add(path);
        else next.delete(path);
        return next;
      });
      if (open) load(path);
    },
    [load]
  );

  // Expands every ancestor of path (the folder being browsed) and loads them.
  const reveal = useCallback(
    (path) => {
      const chain = ancestorsOf(path);
      setExpanded((ex) => {
        const next = new Set(ex);
        chain.forEach((p) => next.add(p));
        return next;
      });
      chain.forEach((p) => load(p));
    },
    [load]
  );

  const reset = useCallback(() => {
    setNodes({});
    setExpanded(new Set([""]));
  }, []);

  return (
    <TreeContext.Provider value={{ nodes, expanded, load, update, invalidate, toggle, reveal, reset }}>
      {children}
    </TreeContext.Provider>
  );
}

export function useTree() {
  const ctx = useContext(TreeContext);
  if (!ctx) throw new Error("useTree must be used within TreeProvider");
  return ctx;
}

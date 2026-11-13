import { useSyncExternalStore } from "react";

/**
 * Paths <-> opaque refs of the browser URLs (/files/<ref>, /edit/<ref>,
 * /view/<ref>): the address bar never shows a real path or file name. The
 * server computes the refs (HMAC under its own key) and sends them with the
 * listings; this module only remembers them, in memory.
 */
const REF_RE = /^[A-Za-z0-9_-]{16}$/;
const byPath = new Map();
const byRef = new Map();
const subs = new Set();
let version = 0;
let pending = false;

export const isRef = (s) => typeof s === "string" && REF_RE.test(s);
export const refOf = (path) => byPath.get(path);
export const pathOf = (ref) => byRef.get(ref);

function changed() {
  version++;
  if (pending) return;
  pending = true;
  queueMicrotask(() => {
    pending = false;
    subs.forEach((f) => f());
  });
}

export function learn(path, ref) {
  if (typeof path !== "string" || !isRef(ref) || byRef.get(ref) === path) return;
  const old = byPath.get(path);
  if (old) byRef.delete(old);
  byPath.set(path, ref);
  byRef.set(ref, path);
  changed();
}

// Picks up {path, ref} pairs of an API result (listing, its entries and
// crumbs, search results, stat, path_resolve).
export function learnFrom(x) {
  if (!x || typeof x !== "object") return;
  if (Array.isArray(x)) {
    x.forEach((e) => e && learn(e.path, e.ref));
    return;
  }
  learn(x.path, x.ref);
  for (const k of ["entries", "crumbs", "results"]) if (Array.isArray(x[k])) learnFrom(x[k]);
}

export function forgetAll() {
  byPath.clear();
  byRef.clear();
  changed();
}

const subscribe = (cb) => {
  subs.add(cb);
  return () => subs.delete(cb);
};

// Re-renders the component when new refs are learned.
export function useRefVersion() {
  return useSyncExternalStore(subscribe, () => version);
}

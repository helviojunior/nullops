import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useApi } from "contexts/AuthContext";
import { isRef, learn, learnFrom, pathOf, refOf, useRefVersion } from "lib/pathref";
import { pathToRoute, routeToPath } from "lib/utils";

/**
 * The path of the current /files|/edit|/view route, whose URL holds a ref.
 * Known refs resolve at once; unknown ones (reload, shared link) ask the
 * server. An old-style URL with the path in it is swapped for its ref.
 *
 * Returns { path, ready, error }: path is null until ready.
 */
export function useRoutePath(prefix) {
  const splat = useParams()["*"] || "";
  const run = useApi();
  const navigate = useNavigate();
  useRefVersion();
  const cached = splat === "" ? "" : isRef(splat) ? pathOf(splat) : undefined;
  const [found, setFound] = useState({ splat: null, path: null, error: "" });

  useEffect(() => {
    if (cached !== undefined) return undefined;
    let alive = true;
    (async () => {
      if (isRef(splat)) {
        try {
          const r = await run((c) => c.call("path_resolve", { ref: splat }));
          learnFrom(r.crumbs);
          learn(r.path, r.ref);
          if (alive) setFound({ splat, path: r.path, error: "" });
          return;
        } catch (e) {
          if (e.status === 401 || !alive) return;
          // not a known ref: maybe an old link with a one-level path
        }
      }
      const p = routeToPath(splat);
      try {
        const r = await run((c) => c.call("path_refs", { paths: [p] }));
        if (!alive) return;
        learn(p, r.refs?.[p]);
        if (refOf(p)) {
          navigate(pathToRoute(prefix, p), { replace: true });
          return;
        }
      } catch (e) {
        if (e.status === 401 || !alive) return;
      }
      if (alive) setFound({ splat, path: p, error: "" });
    })();
    return () => {
      alive = false;
    };
  }, [splat, cached, run, navigate, prefix]);

  if (cached !== undefined) return { path: cached, ready: true, error: "" };
  if (found.splat === splat) return { path: found.path, ready: true, error: found.error };
  return { path: null, ready: false, error: "" };
}

// Route of a path whose ref may not be known yet (a file just created).
export async function routeFor(run, prefix, path) {
  if (path && !refOf(path)) {
    try {
      const r = await run((c) => c.call("path_refs", { paths: [path] }));
      learn(path, r.refs?.[path]);
    } catch {
      /* falls back to the path */
    }
  }
  return pathToRoute(prefix, path);
}

import { clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs) {
  return twMerge(clsx(inputs));
}

export function humanSize(n) {
  if (n === null || n === undefined || isNaN(n)) return "-";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let v = Number(n);
  let i = 0;
  while (Math.abs(v) >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i === 0 ? `${v} B` : `${v.toFixed(1)} ${units[i]}`;
}

// Client paths are slash-separated and relative to the served root ("" = root).
export function joinPath(dir, name) {
  return dir ? `${dir}/${name}` : name;
}

export function parentPath(p) {
  const i = (p || "").lastIndexOf("/");
  return i < 0 ? "" : p.slice(0, i);
}

export function baseName(p) {
  const i = (p || "").lastIndexOf("/");
  return i < 0 ? p : p.slice(i + 1);
}

// Route <-> client path (each segment URL-encoded).
export function pathToRoute(prefix, p) {
  if (!p) return prefix;
  return prefix + "/" + p.split("/").map(encodeURIComponent).join("/");
}

export function routeToPath(splat) {
  if (!splat) return "";
  return splat
    .split("/")
    .filter(Boolean)
    .map((s) => {
      try {
        return decodeURIComponent(s);
      } catch {
        return s;
      }
    })
    .join("/");
}

const ARCHIVE_RE = /\.(zip|tar|tgz|tar\.gz)$/i;
export const isArchive = (name) => ARCHIVE_RE.test(name);

const TEXT_EXT = new Set(
  (
    "txt md markdown log csv tsv json yaml yml toml ini cfg conf env xml html htm css scss less js jsx mjs cjs ts tsx " +
    "go py rb php java kt c h cpp hpp cs rs swift sh bash zsh ps1 psm1 psd1 bat cmd vbs reg sql graphql vue svelte dockerfile makefile gitignore properties tf hcl lua pl r"
  ).split(" ")
);

export function isEditable(name) {
  const low = name.toLowerCase();
  if (!low.includes(".")) return true;
  return TEXT_EXT.has(low.split(".").pop());
}

export function saveBlob(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
}

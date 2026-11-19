import { refOf } from "./pathref";
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

// Client path -> route. The URL carries the path's opaque ref (lib/pathref.js);
// the path itself (each segment URL-encoded) is only the fallback for a ref
// not known yet, which the page then swaps for the ref.
export function pathToRoute(prefix, p) {
  if (!p) return prefix;
  const ref = refOf(p);
  if (ref) return `${prefix}/${ref}`;
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

// ---- clipboard (Ctrl/Cmd+V upload) ----------------------------------------
// Extension to use when saving a clipboard image, by MIME type. Raw clipboard
// images (screenshots, "copy image") arrive as PNG and keep their alpha channel
// because the bytes are uploaded untouched — no canvas re-encode that would
// flatten transparency.
const IMAGE_EXT = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/bmp": "bmp",
  "image/svg+xml": "svg",
  "image/avif": "avif",
};
const imageExt = (type) => IMAGE_EXT[type] || (type || "").split("/")[1] || "png";

// clipboard_YYYY-MM-DD_HH-mm-SS.<ext>
export function clipboardName(type) {
  const d = new Date();
  const p = (n) => String(n).padStart(2, "0");
  const stamp = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}_${p(d.getHours())}-${p(d.getMinutes())}-${p(d.getSeconds())}`;
  return `clipboard_${stamp}.${imageExt(type)}`;
}

// A raw pasted image (screenshot / "copy image") has no real name — browsers
// hand it over empty or as "image.png"/"screenshot.png". A file copied from the
// OS file manager keeps its own name, which is preserved.
const isGeneratedImageName = (name) => !name || /^(image|screenshot)\b.*\.\w+$/i.test(name);

// Pull uploadable files out of a paste event's clipboardData: real files keep
// their name; raw images get a clipboard_<timestamp> name so each paste lands
// as its own distinct file.
export function filesFromClipboard(cd) {
  if (!cd) return [];
  const out = [];
  for (const item of cd.items || []) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (!file) continue;
    if (file.type.startsWith("image/") && isGeneratedImageName(file.name)) {
      out.push(new File([file], clipboardName(file.type), { type: file.type, lastModified: file.lastModified }));
    } else {
      out.push(file);
    }
  }
  // Fallback for paths that expose only a FileList.
  if (!out.length && cd.files?.length) return Array.from(cd.files);
  return out;
}

const IMAGE_RE = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif)$/i;
export const isImage = (name) => IMAGE_RE.test(name);
export const isPdf = (name) => /\.pdf$/i.test(name);
export const isPreviewable = (name) => isImage(name) || isPdf(name) || isEditable(name);

// Preview limits: bigger files are offered for download instead.
export const PREVIEW_MAX_MEDIA = 50 * 1024 * 1024;
export const PREVIEW_MAX_TEXT = 1024 * 1024;

export function previewKind(mime, isText) {
  const m = (mime || "").toLowerCase();
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("application/pdf")) return "pdf";
  if (isText) return "text";
  return "other";
}

// File type groups for the listing filter.
const TYPE_GROUPS = {
  image: IMAGE_RE,
  document: /\.(pdf|docx?|odt|rtf|xlsx?|ods|csv|tsv|pptx?|odp|md|txt)$/i,
  archive: /\.(zip|tar|tgz|gz|bz2|xz|7z|rar)$/i,
  code: /\.(js|jsx|mjs|ts|tsx|go|py|rb|php|java|kt|c|h|cpp|hpp|cs|rs|swift|sh|bash|zsh|ps1|psm1|psd1|bat|cmd|vbs|sql|html?|css|scss|json|ya?ml|toml|xml|ini|conf)$/i,
  media: /\.(mp3|wav|flac|ogg|m4a|mp4|mkv|avi|mov|webm)$/i,
};

export function typeGroup(entry) {
  if (entry.is_dir) return "folder";
  for (const [g, re] of Object.entries(TYPE_GROUPS)) if (re.test(entry.name)) return g;
  return "other";
}

export const TYPE_FILTERS = ["all", "folder", "image", "document", "archive", "code", "media", "other"];

// Sorting: folders first, then by key.
export function sortEntries(entries, key, dir) {
  const mul = dir === "desc" ? -1 : 1;
  const cmp = {
    name: (a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }),
    size: (a, b) => (a.size || 0) - (b.size || 0),
    mtime: (a, b) => (a.mtime || 0) - (b.mtime || 0),
    type: (a, b) => typeGroup(a).localeCompare(typeGroup(b)),
  }[key] || (() => 0);
  return [...entries].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    return cmp(a, b) * mul || a.name.localeCompare(b.name);
  });
}

// File name for display: drops control, bidi and zero-width characters so a
// name cannot disguise itself (e.g. "fdp.exe" shown as "exe.pdf").
// eslint-disable-next-line no-control-regex
const INVISIBLE_RE = /[\u0000-\u001f\u007f-\u009f؜​-‏‪-‮⁠⁦-⁩﻿]/g;
export const displayName = (name) => String(name || "").replace(INVISIBLE_RE, "");

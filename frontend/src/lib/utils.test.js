import { describe, expect, it } from "vitest";
import {
  baseName,
  clipboardName,
  filesFromClipboard,
  humanSize,
  isEditable,
  joinPath,
  parentPath,
  pathToRoute,
  previewKind,
  routeToPath,
  sortEntries,
  typeGroup,
} from "./utils";

describe("paths", () => {
  it("joins and splits client paths", () => {
    expect(joinPath("", "a")).toBe("a");
    expect(joinPath("a/b", "c")).toBe("a/b/c");
    expect(parentPath("a/b/c")).toBe("a/b");
    expect(parentPath("a")).toBe("");
    expect(baseName("a/b/c.txt")).toBe("c.txt");
  });

  it("round-trips routes with special characters", () => {
    const p = "docs/relatório #1/a b?.txt";
    const route = pathToRoute("/files", p);
    expect(route).toBe("/files/docs/relat%C3%B3rio%20%231/a%20b%3F.txt");
    expect(routeToPath(route.slice("/files/".length))).toBe(p);
    expect(pathToRoute("/files", "")).toBe("/files");
  });
});

describe("formatting and types", () => {
  it("formats sizes", () => {
    expect(humanSize(0)).toBe("0 B");
    expect(humanSize(1536)).toBe("1.5 KB");
    expect(humanSize(5 * 1024 * 1024)).toBe("5.0 MB");
  });

  it("strips invisible characters for display", async () => {
    const { displayName } = await import("./utils");
    expect(displayName("inv\u202eoice\u202cfdp.exe")).toBe("invoicefdp.exe");
    expect(displayName("a\nb\u200b.txt")).toBe("ab.txt");
  });

  it("knows editable Windows scripts", () => {
    for (const n of ["a.ps1", "b.vbs", "c.cmd", "d.bat", "Makefile"]) expect(isEditable(n)).toBe(true);
    expect(isEditable("photo.png")).toBe(false);
  });

  it("maps MIME types to preview kinds", () => {
    expect(previewKind("image/png", false)).toBe("image");
    expect(previewKind("image/svg+xml", true)).toBe("image");
    expect(previewKind("application/pdf", false)).toBe("pdf");
    expect(previewKind("text/plain; charset=utf-8", true)).toBe("text");
    expect(previewKind("application/zip", false)).toBe("other");
  });

  it("groups by type", () => {
    expect(typeGroup({ name: "x", is_dir: true })).toBe("folder");
    expect(typeGroup({ name: "a.JPG" })).toBe("image");
    expect(typeGroup({ name: "a.tar.gz" })).toBe("archive");
    expect(typeGroup({ name: "run.ps1" })).toBe("code");
    expect(typeGroup({ name: "weird.xyz" })).toBe("other");
  });
});

describe("clipboard", () => {
  // Minimal stand-ins for DataTransfer / DataTransferItem.
  const item = (file) => ({ kind: file ? "file" : "string", getAsFile: () => file });
  const cd = (files, list) => ({ items: files.map(item), files: list });

  it("names clipboard images clipboard_<timestamp>.<ext> by MIME", () => {
    expect(clipboardName("image/png")).toMatch(/^clipboard_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.png$/);
    expect(clipboardName("image/jpeg")).toMatch(/\.jpg$/);
    expect(clipboardName("image/svg+xml")).toMatch(/\.svg$/);
    expect(clipboardName("")).toMatch(/\.png$/);
  });

  it("renames raw pasted images but keeps real file names", () => {
    const shot = new File([new Uint8Array([1, 2])], "image.png", { type: "image/png" });
    const real = new File([new Uint8Array([3])], "report.pdf", { type: "application/pdf" });
    const photo = new File([new Uint8Array([4])], "vacation.jpg", { type: "image/jpeg" });
    const out = filesFromClipboard(cd([shot, real, photo]));
    expect(out).toHaveLength(3);
    expect(out[0].name).toMatch(/^clipboard_.*\.png$/);
    expect(out[0].type).toBe("image/png");
    expect(out[1].name).toBe("report.pdf"); // untouched
    expect(out[2].name).toBe("vacation.jpg"); // named image file kept
  });

  it("ignores non-file items and falls back to FileList", () => {
    expect(filesFromClipboard(cd([null]))).toEqual([]);
    const f = new File([new Uint8Array([1])], "a.txt", { type: "text/plain" });
    const out = filesFromClipboard({ items: [], files: [f] });
    expect(out.map((x) => x.name)).toEqual(["a.txt"]);
  });

  it("handles missing clipboardData", () => {
    expect(filesFromClipboard(null)).toEqual([]);
  });
});

describe("sortEntries", () => {
  const entries = [
    { name: "b.txt", size: 10, mtime: 3 },
    { name: "file10", size: 1, mtime: 1 },
    { name: "dir", is_dir: true },
    { name: "file2", size: 5, mtime: 2 },
  ];
  it("keeps folders first and sorts names naturally", () => {
    expect(sortEntries(entries, "name", "asc").map((e) => e.name)).toEqual(["dir", "b.txt", "file2", "file10"]);
  });
  it("sorts by size and date in both directions", () => {
    expect(sortEntries(entries, "size", "desc").map((e) => e.name)).toEqual(["dir", "b.txt", "file2", "file10"]);
    expect(sortEntries(entries, "mtime", "asc").map((e) => e.name)).toEqual(["dir", "file10", "file2", "b.txt"]);
  });
});

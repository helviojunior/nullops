import { describe, expect, it } from "vitest";
import {
  baseName,
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

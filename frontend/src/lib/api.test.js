import { describe, expect, it } from "vitest";
import { ApiError, uploadId, withRetry } from "./api";

describe("uploadId", () => {
  it("is stable for the same file and changes otherwise", () => {
    const f = { name: "a.iso", size: 100, lastModified: 1 };
    expect(uploadId(f)).toBe(uploadId({ ...f }));
    expect(uploadId(f)).not.toBe(uploadId({ ...f, size: 101 }));
    expect(uploadId(f)).toMatch(/^[0-9a-f]{32}$/);
  });
});

describe("withRetry", () => {
  it("retries transient errors, not application errors", async () => {
    let n = 0;
    const flaky = () => (++n < 3 ? Promise.reject(new TypeError("network")) : Promise.resolve("ok"));
    expect(await withRetry(flaky, 4, 1)).toBe("ok");
    expect(n).toBe(3);

    let m = 0;
    const appErr = () => {
      m++;
      return Promise.reject(new ApiError("File too large", 200));
    };
    await expect(withRetry(appErr, 4, 1)).rejects.toThrow("File too large");
    expect(m).toBe(1);
  });
});

import { SessionCrypto, b64urlEncode } from "./crypto";

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

// Session-level failures: the UI must go back to the login screen.
export function isSessionError(err) {
  return err instanceof ApiError && err.status === 401;
}

export const UPLOAD_CHUNK = 1024 * 1024; // 1 MiB plaintext per request

export class ApiClient {
  constructor() {
    this.crypto = new SessionCrypto();
    this.authRequired = true;
    this.encrypted = true;
  }

  headers() {
    return { "Content-Type": "application/json", "X-Session-Id": this.crypto.sessionId };
  }

  restore(rec) {
    this.crypto.restore(rec);
    this.encrypted = !rec.plain;
    this.authRequired = true;
  }

  async connect() {
    const info = await this.crypto.handshake();
    this.authRequired = info.authRequired;
    this.encrypted = info.encrypted;
    return info;
  }

  async post(action, payload) {
    const envelope = await this.crypto.encryptJson(payload || {});
    const resp = await fetch("/api/" + action, {
      method: "POST",
      headers: this.headers(),
      body: JSON.stringify(envelope),
    });
    let data;
    try {
      data = await resp.json();
    } catch {
      throw new ApiError("Invalid server response", resp.status);
    }
    // Transport/auth errors come back in clear text (non-200 status).
    if (!resp.ok || (!this.crypto.plain && !data.iv)) {
      throw new ApiError(data.error || "Request failed", resp.status);
    }
    return this.crypto.decryptJson(data);
  }

  // Encrypted action -> result (throws ApiError on {ok:false}).
  async call(action, payload) {
    const out = await this.post(action, payload);
    if (out.ok === false) {
      const e = new ApiError(out.error || "Error", 200);
      e.code = out.code;
      throw e;
    }
    return out.result !== undefined ? out.result : out;
  }

  // Multi-step login: user -> [mfa] -> password. Each returns the raw
  // response ({ok, next, error, ...}).
  loginStart(username) {
    return this.post("login_start", { username });
  }

  loginMFA(code) {
    return this.post("login_mfa", { code });
  }

  loginPassword(username, password) {
    return this.post("login", { username, password });
  }

  async logout() {
    try {
      await this.post("logout", {});
    } catch {
      /* session may already be gone */
    }
  }

  // target: a file path, or {paths: [...]} / a folder -> streamed zip.
  async download(target, onProgress) {
    const payload = typeof target === "string" ? { path: target } : target;
    const envelope = await this.crypto.encryptJson(payload);
    const resp = await fetch("/api/download", {
      method: "POST",
      headers: this.headers(),
      body: JSON.stringify(envelope),
    });
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      throw new ApiError(err.error || "Download failed", resp.status);
    }
    const total = Number(resp.headers.get("X-File-Size") || 0);
    let got = 0;
    return this.crypto.decryptStream(resp.body, (n) => {
      got += n;
      if (onProgress) onProgress(got, total);
    });
  }

  // Resumable, chunked upload. A dropped connection is retried; uploading the
  // same file again (same name, size and date) resumes where it stopped.
  async upload(path, file, onProgress) {
    const base = { path, name: file.name, upload_id: uploadId(file), size: file.size };
    const status = await this.call("upload", { ...base, status: true });
    let offset = Math.min(status.received || 0, file.size);
    if (onProgress && offset) onProgress(offset, file.size);
    for (;;) {
      const end = Math.min(offset + UPLOAD_CHUNK, file.size);
      const bytes = new Uint8Array(await file.slice(offset, end).arrayBuffer());
      const last = end >= file.size;
      const result = await withRetry(() =>
        this.call("upload", { ...base, offset, last, chunk: b64urlEncode(bytes) })
      );
      offset = end;
      if (onProgress) onProgress(offset, file.size);
      if (last) return result;
    }
  }
}

// Stable id for a file: same name + size + modification time -> same id, so
// the server can resume its partial upload. (Not a security token.)
export function uploadId(file) {
  const key = `${file.name}|${file.size}|${file.lastModified || 0}`;
  const h = (seed) => {
    let h1 = 0xdeadbeef ^ seed;
    let h2 = 0x41c6ce57 ^ seed;
    for (let i = 0; i < key.length; i++) {
      const ch = key.charCodeAt(i);
      h1 = Math.imul(h1 ^ ch, 2654435761);
      h2 = Math.imul(h2 ^ ch, 1597334677);
    }
    h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
    h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);
    return (h2 >>> 0).toString(16).padStart(8, "0") + (h1 >>> 0).toString(16).padStart(8, "0");
  };
  return h(1) + h(2);
}

// Retries transient failures (network errors, 5xx); application errors
// ({ok:false}) and auth errors are not retried.
export async function withRetry(fn, attempts = 4, delay = 1000) {
  for (let i = 0; ; i++) {
    try {
      return await fn();
    } catch (err) {
      const transient = !(err instanceof ApiError) || err.status >= 500 || err.status === 0;
      if (!transient || i >= attempts - 1) throw err;
      await new Promise((r) => setTimeout(r, delay * 2 ** i));
    }
  }
}

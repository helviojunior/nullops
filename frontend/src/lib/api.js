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
    if (out.ok === false) throw new ApiError(out.error || "Error", 200);
    return out.result !== undefined ? out.result : out;
  }

  login(username, password) {
    return this.post("login", { username, password });
  }

  async logout() {
    try {
      await this.post("logout", {});
    } catch {
      /* session may already be gone */
    }
  }

  async download(path, onProgress) {
    const envelope = await this.crypto.encryptJson({ path });
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

  async upload(path, file, onProgress) {
    let offset = 0;
    for (;;) {
      const end = Math.min(offset + UPLOAD_CHUNK, file.size);
      const bytes = new Uint8Array(await file.slice(offset, end).arrayBuffer());
      const last = end >= file.size;
      const result = await this.call("upload", {
        path,
        name: file.name,
        offset,
        last,
        chunk: b64urlEncode(bytes),
      });
      offset = end;
      if (onProgress) onProgress(offset, file.size);
      if (last) return result;
    }
  }
}

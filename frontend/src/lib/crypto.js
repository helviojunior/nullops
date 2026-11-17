// Transport encryption for the /api channel (browser side).
// Mirrors pkg/crypto in the Go server:
//   ECDH P-256 -> HKDF-SHA256(salt = session id bytes, info = "nullops/aesgcm/v1")
//   -> AES-256-GCM.
// The AES key never leaves this CryptoKey object (non-extractable, memory only).

const BLOB_BATCH = 32 * 1024 * 1024;

const HKDF_INFO = new TextEncoder().encode("nullops/aesgcm/v1");

export function b64urlEncode(bytes) {
  const arr = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let bin = "";
  const step = 0x8000;
  for (let i = 0; i < arr.length; i += step) {
    bin += String.fromCharCode.apply(null, arr.subarray(i, i + step));
  }
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function b64urlDecode(str) {
  let s = str.replace(/-/g, "+").replace(/_/g, "/");
  while (s.length % 4) s += "=";
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function hexToBytes(hex) {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}

// ---- obfuscated channel (--no-tls fallback) -------------------------------
// This is NOT encryption. Over plain http window.crypto.subtle is unavailable,
// so the browser cannot do AES-GCM. The obfuscated mode XORs payloads with a
// SHA-256 counter-mode keystream, mirroring pkg/crypto/obfuscate.go. Keyed on
// the session id, it is cosmetic: it keeps payloads out of cleartext against
// passive inspection and logs, but a deliberate observer who saw the handshake
// can reproduce the keystream, and without TLS an active MITM is unstoppable.

const OBF_INFO = new TextEncoder().encode("nullops/obf/v1");

function ror(x, n) {
  return (x >>> n) | (x << (32 - n));
}

const SHA256_K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

// sha256 hashes a Uint8Array and returns 32 bytes (FIPS 180-4).
export function sha256(msg) {
  const h = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);
  const l = msg.length;
  const k = (56 - ((l + 1) % 64) + 64) % 64;
  const total = l + 1 + k + 8;
  const m = new Uint8Array(total);
  m.set(msg);
  m[l] = 0x80;
  const dv = new DataView(m.buffer);
  const bits = l * 8;
  dv.setUint32(total - 8, Math.floor(bits / 0x100000000));
  dv.setUint32(total - 4, bits >>> 0);

  const w = new Uint32Array(64);
  for (let off = 0; off < total; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = dv.getUint32(off + i * 4);
    for (let i = 16; i < 64; i++) {
      const s0 = ror(w[i - 15], 7) ^ ror(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = ror(w[i - 2], 17) ^ ror(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const S1 = ror(e, 6) ^ ror(e, 11) ^ ror(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (hh + S1 + ch + SHA256_K[i] + w[i]) >>> 0;
      const S0 = ror(a, 2) ^ ror(a, 13) ^ ror(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) >>> 0;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    h[0] = (h[0] + a) >>> 0;
    h[1] = (h[1] + b) >>> 0;
    h[2] = (h[2] + c) >>> 0;
    h[3] = (h[3] + d) >>> 0;
    h[4] = (h[4] + e) >>> 0;
    h[5] = (h[5] + f) >>> 0;
    h[6] = (h[6] + g) >>> 0;
    h[7] = (h[7] + hh) >>> 0;
  }
  const out = new Uint8Array(32);
  const odv = new DataView(out.buffer);
  for (let i = 0; i < 8; i++) odv.setUint32(i * 4, h[i]);
  return out;
}

export class Obfuscator {
  constructor(secret) {
    this.secret = secret instanceof Uint8Array ? secret : new Uint8Array(secret);
  }

  // xor applies the keystream to buf in place, returning it.
  xor(nonce, buf) {
    const ctr = new Uint8Array(4);
    let block = null;
    let off = 32;
    let n = 0;
    for (let i = 0; i < buf.length; i++) {
      if (off === 32) {
        ctr[0] = n & 0xff;
        ctr[1] = (n >>> 8) & 0xff;
        ctr[2] = (n >>> 16) & 0xff;
        ctr[3] = (n >>> 24) & 0xff;
        const pre = new Uint8Array(OBF_INFO.length + this.secret.length + nonce.length + 4);
        let p = 0;
        pre.set(OBF_INFO, p);
        p += OBF_INFO.length;
        pre.set(this.secret, p);
        p += this.secret.length;
        pre.set(nonce, p);
        p += nonce.length;
        pre.set(ctr, p);
        block = sha256(pre);
        n++;
        off = 0;
      }
      buf[i] ^= block[off];
      off++;
    }
    return buf;
  }

  seal(bytes) {
    const nonce = window.crypto.getRandomValues(new Uint8Array(12));
    const data = this.xor(nonce, bytes.slice());
    return { iv: b64urlEncode(nonce), data: b64urlEncode(data) };
  }

  open(env) {
    return this.xor(b64urlDecode(env.iv), b64urlDecode(env.data));
  }
}

export function secureContextAvailable() {
  return !!(window.isSecureContext && window.crypto && window.crypto.subtle);
}

export class InsecureContextError extends Error {
  constructor() {
    super("Secure context required");
    this.name = "InsecureContextError";
  }
}

/**
 * Session crypto. When the server runs with --no-tls it answers the
 * handshake with {encrypted:false}: the session is then "plain" and every
 * method below is a pass-through (no WebCrypto needed).
 */
export class SessionCrypto {
  constructor() {
    this.sessionId = null;
    this.aesKey = null;
    this.obf = null;
    this.plain = false;
  }

  async handshake() {
    const subtle = secureContextAvailable() ? window.crypto.subtle : null;
    let pair = null;
    // Prefer encryption when WebCrypto is available (secure context), otherwise
    // offer the pure-JS fallbacks in order. The server picks the first mode it
    // also supports and echoes it back as `mode`.
    const body = { accept: subtle ? ["encrypted"] : ["obfuscated", "cleartext"] };
    if (subtle) {
      pair = await subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, false, ["deriveBits"]);
      body.client_pub = b64urlEncode(await subtle.exportKey("raw", pair.publicKey));
    }

    const resp = await fetch("/api/handshake", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await resp.json().catch(() => ({}));
    if (data.code === "secure_context") throw new InsecureContextError();
    if (!resp.ok) throw new Error(data.error || "Handshake failed");

    // Back-compat with a server that predates mode negotiation.
    const mode = data.mode || (data.encrypted === false ? "cleartext" : "encrypted");

    if (mode === "cleartext") {
      this.plain = true;
      this.sessionId = data.session_id;
      return { authRequired: !!data.auth_required, encrypted: false, mode };
    }
    if (mode === "obfuscated") {
      this.sessionId = data.session_id;
      this.obf = new Obfuscator(hexToBytes(data.session_id));
      return { authRequired: !!data.auth_required, encrypted: false, mode };
    }
    if (!subtle) throw new InsecureContextError();

    const serverPub = await subtle.importKey(
      "raw",
      b64urlDecode(data.server_pub),
      { name: "ECDH", namedCurve: "P-256" },
      false,
      []
    );
    const shared = await subtle.deriveBits({ name: "ECDH", public: serverPub }, pair.privateKey, 256);
    const hkdfKey = await subtle.importKey("raw", shared, "HKDF", false, ["deriveKey"]);
    this.aesKey = await subtle.deriveKey(
      { name: "HKDF", hash: "SHA-256", salt: hexToBytes(data.session_id), info: HKDF_INFO },
      hkdfKey,
      { name: "AES-GCM", length: 256 },
      false,
      ["encrypt", "decrypt"]
    );
    this.sessionId = data.session_id;
    return { authRequired: !!data.auth_required, encrypted: true, mode: "encrypted" };
  }

  // Restores a persisted session (see lib/sessionStore.js).
  restore(rec) {
    this.sessionId = rec.sessionId;
    this.plain = rec.mode ? rec.mode === "cleartext" : !!rec.plain;
    this.aesKey = this.plain || rec.mode === "obfuscated" ? null : rec.aesKey;
    this.obf = rec.mode === "obfuscated" ? new Obfuscator(hexToBytes(rec.sessionId)) : null;
  }

  snapshot() {
    const mode = this.obf ? "obfuscated" : this.plain ? "cleartext" : "encrypted";
    return { sessionId: this.sessionId, plain: this.plain, aesKey: this.aesKey, mode };
  }

  async encryptJson(obj) {
    if (this.plain) return obj;
    if (this.obf) return this.obf.seal(new TextEncoder().encode(JSON.stringify(obj)));
    const iv = window.crypto.getRandomValues(new Uint8Array(12));
    const pt = new TextEncoder().encode(JSON.stringify(obj));
    const ct = await window.crypto.subtle.encrypt({ name: "AES-GCM", iv }, this.aesKey, pt);
    return { iv: b64urlEncode(iv), data: b64urlEncode(ct) };
  }

  async decryptJson(envelope) {
    if (this.plain) return envelope;
    if (this.obf) return JSON.parse(new TextDecoder().decode(this.obf.open(envelope)));
    const pt = await window.crypto.subtle.decrypt(
      { name: "AES-GCM", iv: b64urlDecode(envelope.iv) },
      this.aesKey,
      b64urlDecode(envelope.data)
    );
    return JSON.parse(new TextDecoder().decode(pt));
  }

  // Streams and decrypts length-prefixed frames:
  //   [4-byte BE len][12-byte iv][len bytes ct||tag]
  // With a sink (a FileSystemWritableFileStream) every decrypted chunk is
  // written straight to disk and nothing is kept in memory. Without one, the
  // chunks are packed into Blobs every BLOB_BATCH bytes, so the data leaves
  // the JavaScript heap (the browser may keep large Blobs on disk) instead of
  // piling up as one huge array.
  async decryptStream(body, onBytes, signal, sink) {
    const reader = body.getReader();
    const check = () => {
      if (signal?.aborted) {
        reader.cancel().catch(() => {});
        throw new DOMException("Transfer canceled", "AbortError");
      }
    };
    const blobs = [];
    let parts = [];
    let pending = 0;
    const emit = async (chunk) => {
      if (onBytes) onBytes(chunk.byteLength);
      if (sink) return sink.write(chunk);
      parts.push(chunk);
      pending += chunk.byteLength;
      if (pending >= BLOB_BATCH) {
        blobs.push(new Blob(parts));
        parts = [];
        pending = 0;
      }
    };
    const finish = () => {
      if (sink) return null;
      if (parts.length) blobs.push(new Blob(parts));
      return new Blob(blobs);
    };

    if (this.plain) {
      for (;;) {
        const { done, value } = await reader.read();
        check();
        if (value) await emit(value);
        if (done) return finish();
      }
    }
    let buf = new Uint8Array(0);
    for (;;) {
      const { done, value } = await reader.read();
      check();
      if (value) {
        const merged = new Uint8Array(buf.length + value.length);
        merged.set(buf);
        merged.set(value, buf.length);
        buf = merged;
        let off = 0;
        while (buf.length - off >= 16) {
          const n = new DataView(buf.buffer, buf.byteOffset + off, 4).getUint32(0, false);
          if (buf.length - off < 16 + n) break;
          const iv = buf.slice(off + 4, off + 16);
          const ct = buf.subarray(off + 16, off + 16 + n);
          // Obfuscated frames carry no auth tag, so n is the payload length and
          // the transform is the synchronous keystream XOR; the geometry
          // (4-byte len + 12-byte nonce) is identical to the AES-GCM frame.
          const pt = this.obf
            ? this.obf.xor(iv, ct.slice())
            : new Uint8Array(await window.crypto.subtle.decrypt({ name: "AES-GCM", iv }, this.aesKey, ct));
          await emit(pt);
          off += 16 + n;
          check();
        }
        buf = buf.slice(off);
      }
      if (done) break;
    }
    if (buf.length) throw new Error("Truncated download");
    return finish();
  }
}

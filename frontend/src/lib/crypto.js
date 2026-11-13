// Transport encryption for the /api channel (browser side).
// Mirrors pkg/crypto in the Go server:
//   ECDH P-256 -> HKDF-SHA256(salt = session id bytes, info = "fmweb/aesgcm/v1")
//   -> AES-256-GCM.
// The AES key never leaves this CryptoKey object (non-extractable, memory only).

const HKDF_INFO = new TextEncoder().encode("fmweb/aesgcm/v1");

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

export function secureContextAvailable() {
  return !!(window.isSecureContext && window.crypto && window.crypto.subtle);
}

export class SessionCrypto {
  constructor() {
    this.sessionId = null;
    this.aesKey = null;
  }

  async handshake() {
    const subtle = window.crypto.subtle;
    const pair = await subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, false, [
      "deriveBits",
    ]);
    const clientPubRaw = await subtle.exportKey("raw", pair.publicKey);

    const resp = await fetch("/api/handshake", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ client_pub: b64urlEncode(clientPubRaw) }),
    });
    if (!resp.ok) throw new Error("Handshake failed");
    const data = await resp.json();

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
    return { authRequired: !!data.auth_required };
  }

  async encryptJson(obj) {
    const iv = window.crypto.getRandomValues(new Uint8Array(12));
    const pt = new TextEncoder().encode(JSON.stringify(obj));
    const ct = await window.crypto.subtle.encrypt({ name: "AES-GCM", iv }, this.aesKey, pt);
    return { iv: b64urlEncode(iv), data: b64urlEncode(ct) };
  }

  async decryptJson(envelope) {
    const pt = await window.crypto.subtle.decrypt(
      { name: "AES-GCM", iv: b64urlDecode(envelope.iv) },
      this.aesKey,
      b64urlDecode(envelope.data)
    );
    return JSON.parse(new TextDecoder().decode(pt));
  }

  // Streams and decrypts length-prefixed frames:
  //   [4-byte BE len][12-byte iv][len bytes ct||tag]
  async decryptStream(body, onBytes) {
    const reader = body.getReader();
    let buf = new Uint8Array(0);
    const parts = [];
    for (;;) {
      const { done, value } = await reader.read();
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
          const ct = buf.slice(off + 16, off + 16 + n);
          const pt = await window.crypto.subtle.decrypt({ name: "AES-GCM", iv }, this.aesKey, ct);
          parts.push(new Uint8Array(pt));
          if (onBytes) onBytes(pt.byteLength);
          off += 16 + n;
        }
        buf = buf.slice(off);
      }
      if (done) break;
    }
    if (buf.length) throw new Error("Truncated download");
    return new Blob(parts);
  }
}

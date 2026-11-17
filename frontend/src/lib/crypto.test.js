import { beforeAll, describe, expect, it } from "vitest";

// crypto.js targets the browser (window.crypto); Node 22 provides WebCrypto.
beforeAll(() => {
  globalThis.window = globalThis;
  globalThis.isSecureContext = true;
});

describe("SessionCrypto", () => {
  it("encrypts and decrypts envelopes and frame streams", async () => {
    const { SessionCrypto, b64urlDecode, b64urlEncode } = await import("./crypto");
    const key = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
    const sc = new SessionCrypto();
    sc.restore({ sessionId: "00", aesKey: key, plain: false });

    const env = await sc.encryptJson({ hello: "mundo", n: 1 });
    expect(env.iv).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(await sc.decryptJson(env)).toEqual({ hello: "mundo", n: 1 });

    // build two frames like the server: [len][iv][ct||tag]
    const frame = async (text) => {
      const iv = crypto.getRandomValues(new Uint8Array(12));
      const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, new TextEncoder().encode(text)));
      const out = new Uint8Array(16 + ct.length);
      new DataView(out.buffer).setUint32(0, ct.length);
      out.set(iv, 4);
      out.set(ct, 16);
      return out;
    };
    const f1 = await frame("abc"), f2 = await frame("def");
    const all = new Uint8Array(f1.length + f2.length);
    all.set(f1);
    all.set(f2, f1.length);
    // deliver in odd-sized pieces to exercise reassembly
    const body = new ReadableStream({
      start(c) {
        for (let i = 0; i < all.length; i += 7) c.enqueue(all.slice(i, i + 7));
        c.close();
      },
    });
    const blob = await sc.decryptStream(body);
    expect(await blob.text()).toBe("abcdef");

    expect(b64urlDecode(b64urlEncode(new Uint8Array([0, 255, 128])))).toEqual(new Uint8Array([0, 255, 128]));
  });

  it("is a pass-through for plaintext sessions", async () => {
    const { SessionCrypto } = await import("./crypto");
    const sc = new SessionCrypto();
    sc.restore({ sessionId: "00", plain: true });
    expect(await sc.encryptJson({ a: 1 })).toEqual({ a: 1 });
    expect(await sc.decryptJson({ a: 1 })).toEqual({ a: 1 });
  });
});

describe("obfuscated channel", () => {
  const enc = (s) => new TextEncoder().encode(s);
  const hex = (b) => [...b].map((x) => x.toString(16).padStart(2, "0")).join("");

  it("computes SHA-256 (FIPS 180-4 vector)", async () => {
    const { sha256 } = await import("./crypto");
    expect(hex(sha256(enc("abc")))).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(hex(sha256(new Uint8Array(0)))).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
  });

  it("matches the Go keystream vector", async () => {
    const { Obfuscator } = await import("./crypto");
    const o = new Obfuscator(enc("nullops-test-secret"));
    const nonce = new Uint8Array([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
    const out = o.xor(nonce, enc("hello world"));
    expect(hex(out)).toBe("243f58258653727c088c1b"); // pkg/crypto TestObfuscateVector
  });

  it("round-trips envelopes and JSON through SessionCrypto", async () => {
    const { SessionCrypto } = await import("./crypto");
    const sc = new SessionCrypto();
    sc.restore({ sessionId: "00112233445566778899aabbccddeeff", mode: "obfuscated" });
    const env = await sc.encryptJson({ hello: "mundo", n: 7 });
    expect(env.iv).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(JSON.stringify(env)).not.toContain("mundo");
    expect(await sc.decryptJson(env)).toEqual({ hello: "mundo", n: 7 });
  });

  it("decrypts a stream of obfuscated frames", async () => {
    const { SessionCrypto, Obfuscator } = await import("./crypto");
    const sid = "00112233445566778899aabbccddeeff";
    const sc = new SessionCrypto();
    sc.restore({ sessionId: sid, mode: "obfuscated" });
    const secret = new Uint8Array(16);
    for (let i = 0; i < 16; i++) secret[i] = parseInt(sid.substr(i * 2, 2), 16);
    const server = new Obfuscator(secret);
    const frame = (text) => {
      const iv = crypto.getRandomValues(new Uint8Array(12));
      const payload = server.xor(iv, enc(text));
      const out = new Uint8Array(16 + payload.length);
      new DataView(out.buffer).setUint32(0, payload.length);
      out.set(iv, 4);
      out.set(payload, 16);
      return out;
    };
    const f1 = frame("abc"),
      f2 = frame("defgh");
    const all = new Uint8Array(f1.length + f2.length);
    all.set(f1);
    all.set(f2, f1.length);
    const body = new ReadableStream({
      start(c) {
        for (let i = 0; i < all.length; i += 5) c.enqueue(all.slice(i, i + 5));
        c.close();
      },
    });
    const blob = await sc.decryptStream(body);
    expect(await blob.text()).toBe("abcdefgh");
  });
});

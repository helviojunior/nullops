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

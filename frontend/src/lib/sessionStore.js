// Keeps the authenticated session across page reloads and new tabs.
//
// The AES key is stored as a NON-EXTRACTABLE CryptoKey in IndexedDB: the
// browser can use it to encrypt/decrypt, but no script can read its bytes.
// The record is removed on logout or when the server rejects the session.

const DB = "fmweb";
const STORE = "session";
const KEY = "current";

function open() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function tx(mode, fn) {
  const db = await open();
  try {
    return await new Promise((resolve, reject) => {
      const t = db.transaction(STORE, mode);
      const req = fn(t.objectStore(STORE));
      t.oncomplete = () => resolve(req && req.result);
      t.onerror = () => reject(t.error);
      t.onabort = () => reject(t.error);
    });
  } finally {
    db.close();
  }
}

export async function saveSession(rec) {
  try {
    await tx("readwrite", (s) => s.put(rec, KEY));
  } catch {
    /* private mode / storage disabled: session stays in memory only */
  }
}

export async function loadSession() {
  try {
    const rec = await tx("readonly", (s) => s.get(KEY));
    if (!rec || !rec.sessionId) return null;
    if (rec.expiresAt && rec.expiresAt * 1000 < Date.now()) {
      await clearSession();
      return null;
    }
    return rec;
  } catch {
    return null;
  }
}

export async function clearSession() {
  try {
    await tx("readwrite", (s) => s.delete(KEY));
  } catch {
    /* ignore */
  }
}

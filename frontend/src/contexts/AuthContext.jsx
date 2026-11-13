import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { ApiClient, ApiError, isSessionError } from "lib/api";
import { InsecureContextError } from "lib/crypto";
import { clearSession, loadSession, saveSession } from "lib/sessionStore";

/**
 * Owns the encrypted channel. After login the session (id + non-extractable
 * AES key) is kept in IndexedDB, so reloads and new tabs reuse it until it
 * expires (12h by default) or the user signs out.
 */
const AuthContext = createContext(null);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export function AuthProvider({ children }) {
  const clientRef = useRef(null);
  const [status, setStatus] = useState("connecting"); // connecting | ready | error | insecure
  const [user, setUser] = useState(null); // {username, readonly, title, version, auth}
  const [expired, setExpired] = useState(false);
  const [encrypted, setEncrypted] = useState(true);

  const loadInfo = useCallback(async (client) => {
    const info = await client.call("info", {});
    setUser(info);
    if (info.title) document.title = info.title;
    return info;
  }, []);

  // Tries to resume a persisted session; false when there is none / it expired.
  const resume = useCallback(async () => {
    const rec = await loadSession();
    if (!rec) return false;
    const client = new ApiClient();
    client.restore(rec);
    try {
      await loadInfo(client);
    } catch (err) {
      if (isSessionError(err)) await clearSession();
      return false;
    }
    clientRef.current = client;
    setEncrypted(client.encrypted);
    setStatus("ready");
    return true;
  }, [loadInfo]);

  // Handshake with retries (absorbs a server that is still starting).
  const connect = useCallback(async () => {
    setStatus("connecting");
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const client = new ApiClient();
        await client.connect();
        clientRef.current = client;
        setEncrypted(client.encrypted);
        if (!client.authRequired) await loadInfo(client);
        setStatus("ready");
        return client;
      } catch (err) {
        if (err instanceof InsecureContextError) {
          setStatus("insecure");
          return null;
        }
        await sleep(1500);
      }
    }
    setStatus("error");
    return null;
  }, [loadInfo]);

  useEffect(() => {
    (async () => {
      if (!(await resume())) await connect();
    })();
  }, [connect, resume]);

  const login = useCallback(
    async (username, password) => {
      let client = clientRef.current;
      if (!client) client = await connect();
      if (!client) throw new ApiError("Service unavailable", 0);
      let out;
      try {
        out = await client.login(username, password);
      } catch (err) {
        if (isSessionError(err)) {
          // the server forgot our session (restart / TTL): new handshake
          client = await connect();
          if (!client) throw err;
          out = await client.login(username, password);
        } else {
          throw err;
        }
      }
      if (!out.ok) {
        const e = new ApiError(out.error || "Invalid credentials", 200);
        e.retryAfter = out.retry_after;
        throw e;
      }
      setExpired(false);
      await saveSession({ ...client.crypto.snapshot(), expiresAt: out.expires_at || 0 });
      return loadInfo(client);
    },
    [connect, loadInfo]
  );

  const logout = useCallback(async () => {
    const client = clientRef.current;
    clientRef.current = null;
    setUser(null);
    await clearSession();
    if (client) await client.logout();
    connect();
  }, [connect]);

  // Called when any request reports a lost session.
  const sessionLost = useCallback(() => {
    clearSession();
    clientRef.current = null;
    setUser(null);
    setExpired(true);
    connect();
  }, [connect]);

  const value = {
    status,
    user,
    expired,
    encrypted,
    authRequired: clientRef.current ? clientRef.current.authRequired : true,
    client: () => clientRef.current,
    connect,
    login,
    logout,
    sessionLost,
  };
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}

/**
 * useApi(): returns call(action, payload) that routes session loss back to the
 * login screen and translates server error messages.
 */
export function useApi() {
  const { client, sessionLost } = useAuth();
  return useCallback(
    async (fn) => {
      const c = client();
      if (!c) {
        sessionLost();
        throw new ApiError("No session", 401);
      }
      try {
        return await fn(c);
      } catch (err) {
        if (isSessionError(err)) sessionLost();
        throw err;
      }
    },
    [client, sessionLost]
  );
}

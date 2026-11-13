import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { ApiClient, ApiError, isSessionError } from "lib/api";
import { secureContextAvailable } from "lib/crypto";

/**
 * Owns the encrypted channel. The session key lives only in this JS context:
 * a page reload starts a new handshake (and requires signing in again).
 */
const AuthContext = createContext(null);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export function AuthProvider({ children }) {
  const clientRef = useRef(null);
  const [status, setStatus] = useState("connecting"); // connecting | ready | error | insecure
  const [user, setUser] = useState(null); // {username, readonly, title, version, auth}
  const [expired, setExpired] = useState(false);

  const loadInfo = useCallback(async (client) => {
    const info = await client.call("info", {});
    setUser(info);
    if (info.title) document.title = info.title;
    return info;
  }, []);

  // Handshake with retries (absorbs a server that is still starting).
  const connect = useCallback(async () => {
    if (!secureContextAvailable()) {
      setStatus("insecure");
      return null;
    }
    setStatus("connecting");
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const client = new ApiClient();
        await client.connect();
        clientRef.current = client;
        if (!client.authRequired) await loadInfo(client);
        setStatus("ready");
        return client;
      } catch {
        await sleep(1500);
      }
    }
    setStatus("error");
    return null;
  }, [loadInfo]);

  useEffect(() => {
    connect();
  }, [connect]);

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
      return loadInfo(client);
    },
    [connect, loadInfo]
  );

  const logout = useCallback(async () => {
    const client = clientRef.current;
    clientRef.current = null;
    setUser(null);
    if (client) await client.logout();
    connect();
  }, [connect]);

  // Called when any request reports a lost session.
  const sessionLost = useCallback(() => {
    clientRef.current = null;
    setUser(null);
    setExpired(true);
    connect();
  }, [connect]);

  const value = {
    status,
    user,
    expired,
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

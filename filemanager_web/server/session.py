# -*- coding: utf-8 -*-
"""In-memory session store.

Every session holds a per-session AES-GCM cipher (derived during the ECDH
handshake) and lives only in process memory. Nothing is persisted to disk.
Sessions are pruned on access once idle past the configured TTL.
"""
import secrets
import threading
import time

from .crypto import SessionCipher, derive_key, generate_keypair


class Session(object):
    __slots__ = ('id', 'cipher', 'created', 'last_seen',
                 'authenticated', 'username', 'readonly', 'home')

    def __init__(self, sid, cipher):
        now = time.time()
        self.id = sid
        self.cipher = cipher
        self.created = now
        self.last_seen = now
        self.authenticated = False
        self.username = None
        self.readonly = False
        self.home = None

    def touch(self):
        self.last_seen = time.time()


class SessionManager(object):
    def __init__(self, ttl_seconds=3600):
        self.ttl = ttl_seconds
        self._sessions = {}
        self._lock = threading.Lock()

    def create(self, client_public_bytes):
        """Run the server side of the ECDH handshake and create a session.

        Returns (session, server_public_bytes).
        """
        sid_bytes = secrets.token_bytes(16)
        sid = sid_bytes.hex()
        private_key, server_public = generate_keypair()
        key = derive_key(private_key, client_public_bytes, salt=sid_bytes)
        session = Session(sid, SessionCipher(key))
        with self._lock:
            self._prune_locked()
            self._sessions[sid] = session
        return session, server_public

    def get(self, sid):
        if not sid:
            return None
        with self._lock:
            session = self._sessions.get(sid)
            if session is None:
                return None
            if time.time() - session.last_seen > self.ttl:
                self._sessions.pop(sid, None)
                return None
            session.touch()
            return session

    def destroy(self, sid):
        with self._lock:
            self._sessions.pop(sid, None)

    def _prune_locked(self):
        now = time.time()
        stale = [s for s, v in self._sessions.items()
                 if now - v.last_seen > self.ttl]
        for s in stale:
            self._sessions.pop(s, None)

    def count(self):
        with self._lock:
            return len(self._sessions)

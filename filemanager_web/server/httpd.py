# -*- coding: utf-8 -*-
"""HTTP server, router and request handler (stdlib http.server only).

Routing:
  GET  /                      -> web/index.html (single-page app)
  GET  /css/* /js/* /*.ico    -> static front-end assets (bundled package data)
  POST /api/handshake         -> ECDH handshake (the only plaintext endpoint)
  POST /api/login             -> encrypted {username,password}
  POST /api/logout
  POST /api/download          -> encrypted {path}; response = encrypted frames
  POST /api/upload            -> encrypted chunk envelope; appends to .part file
  POST /api/<action>          -> encrypted JSON action (see server/api.py)

Every /api endpoint except /handshake rides inside an AES-GCM envelope keyed by
the session established during the handshake. Session keys live only in memory.
"""
import json
import os
import posixpath
import socketserver
import sys
from http.server import BaseHTTPRequestHandler

from .api import Api, ApiError
from .crypto import b64url_decode, b64url_encode
from ..util.logger import Logger
from ..util.tools import Tools

_WEB_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'web')

_CONTENT_TYPES = {
    '.html': 'text/html; charset=utf-8',
    '.css': 'text/css; charset=utf-8',
    '.js': 'application/javascript; charset=utf-8',
    '.ico': 'image/x-icon',
    '.svg': 'image/svg+xml',
    '.png': 'image/png',
    '.json': 'application/json; charset=utf-8',
}

_MAX_JSON_BODY = 64 * 1024 * 1024  # cap for a single JSON/envelope POST
_DOWNLOAD_CHUNK = 256 * 1024


class Handler(BaseHTTPRequestHandler):
    server_version = 'filemanager-web'
    protocol_version = 'HTTP/1.1'

    # config + api injected via the server instance
    @property
    def config(self):
        return self.server.config

    @property
    def api(self):
        return self.server.api

    # -- logging ----------------------------------------------------------
    def log_message(self, fmt, *args):
        Logger.debug('%s - %s' % (self.address_string(), fmt % args))

    def log_error(self, fmt, *args):
        Logger.debug('%s - %s' % (self.address_string(), fmt % args))

    # -- small helpers ----------------------------------------------------
    def _read_raw_body(self, limit=_MAX_JSON_BODY):
        """Read the entire request body exactly once.

        The whole body MUST be consumed before we respond; otherwise, on a
        keep-alive connection, the unread bytes are parsed as the next request
        line (producing a bogus 501). We therefore drain it up-front in
        do_POST and cache it for the handlers.
        """
        length = int(self.headers.get('Content-Length') or 0)
        if length <= 0:
            self._raw_body = b''
            return self._raw_body
        if length > limit:
            # Still drain so the connection stays usable, then reject.
            remaining = length
            while remaining > 0:
                chunk = self.rfile.read(min(remaining, 65536))
                if not chunk:
                    break
                remaining -= len(chunk)
            self._raw_body = b''
            raise ApiError('Payload too large', status=413)
        self._raw_body = self.rfile.read(length)
        return self._raw_body

    def _read_body(self, limit=_MAX_JSON_BODY):
        # Body was already read and cached by do_POST.
        return getattr(self, '_raw_body', b'')

    def _send_json(self, obj, status=200):
        body = json.dumps(obj).encode('utf-8')
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Cache-Control', 'no-store')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.end_headers()
        self.wfile.write(body)

    def _send_encrypted(self, session, obj, status=200):
        envelope = session.cipher.encrypt_json(obj)
        self._send_json(envelope, status=status)

    def _send_error_json(self, message, status=400):
        self._send_json({'ok': False, 'error': message}, status=status)

    def _get_session(self):
        sid = self.headers.get('X-Session-Id')
        return self.config.sessions.get(sid)

    # -- dispatch ---------------------------------------------------------
    def do_GET(self):
        path = self.path.split('?', 1)[0]
        if path == '/' or path == '':
            return self._serve_static('index.html')
        if path.startswith('/api/'):
            return self._send_error_json('Use POST for /api', status=405)
        return self._serve_static(path.lstrip('/'))

    def do_POST(self):
        path = self.path.split('?', 1)[0]
        # Always drain the request body first so keep-alive connections never
        # mis-parse an unread body as the next request.
        try:
            self._read_raw_body()
        except ApiError as exc:
            return self._send_error_json(str(exc), status=exc.status)
        if not path.startswith('/api/'):
            return self._send_error_json('Not found', status=404)
        action = path[len('/api/'):].strip('/')
        try:
            if action == 'handshake':
                return self._handle_handshake()
            if action == 'download':
                return self._handle_download()
            if action == 'upload':
                return self._handle_upload()
            return self._handle_api(action)
        except ApiError as exc:
            return self._send_error_json(str(exc), status=exc.status)
        except BrokenPipeError:
            pass
        except Exception as exc:  # noqa: BLE001
            Logger.error('Unhandled error: %s' % exc)
            return self._send_error_json('Internal error', status=500)

    # -- handshake (plaintext) -------------------------------------------
    def _handle_handshake(self):
        raw = self._read_body()
        if len(raw) > 4096:
            raise ApiError('Malformed handshake', status=400)
        try:
            data = json.loads(raw.decode('utf-8'))
            client_pub = b64url_decode(data['client_pub'])
        except Exception:
            raise ApiError('Malformed handshake', status=400)
        try:
            session, server_pub = self.config.sessions.create(client_pub)
        except Exception:
            raise ApiError('Handshake failed', status=400)
        # If auth is disabled, every session is immediately authenticated.
        if not self.config.use_auth:
            session.authenticated = True
            session.username = 'anonymous'
            session.readonly = self.config.global_readonly
        self._send_json({
            'session_id': session.id,
            'server_pub': b64url_encode(server_pub),
            'auth_required': self.config.use_auth,
        })

    # -- authenticated JSON api ------------------------------------------
    def _decrypt_request(self, session):
        raw = self._read_body()
        try:
            envelope = json.loads(raw.decode('utf-8'))
            return session.cipher.decrypt_json(envelope)
        except Exception:
            raise ApiError('Decryption failed', status=400)

    def _handle_api(self, action):
        session = self._get_session()
        if session is None:
            raise ApiError('No session', status=401)

        if action == 'login':
            return self._handle_login(session)
        if action == 'logout':
            self.config.sessions.destroy(session.id)
            return self._send_encrypted(session, {'ok': True})

        if self.config.use_auth and not session.authenticated:
            raise ApiError('Not authenticated', status=401)

        payload = self._decrypt_request(session)
        try:
            result = self.api.dispatch(action, payload, session)
        except ApiError as exc:
            # Application-level errors ride inside the encrypted body with a
            # 200 status; the client inspects {ok:false}. HTTP status codes
            # are reserved for transport/auth failures so the browser console
            # stays clean during normal use.
            return self._send_encrypted(session, {'ok': False,
                                                  'error': str(exc)})
        self._send_encrypted(session, {'ok': True, 'result': result})

    def _handle_login(self, session):
        payload = self._decrypt_request(session)
        if not self.config.use_auth:
            session.authenticated = True
            return self._send_encrypted(session, {'ok': True,
                                                  'username': 'anonymous',
                                                  'readonly': self.config.global_readonly})
        user = self.config.users.verify(payload.get('username', ''),
                                        payload.get('password', ''))
        if user is None:
            # 200 on purpose: the failure is carried inside the encrypted
            # body, so the browser console stays clean. The client reads ok.
            return self._send_encrypted(session, {'ok': False,
                                                  'error': 'Invalid credentials'})
        session.authenticated = True
        session.username = user.name
        session.readonly = user.readonly or self.config.global_readonly
        self._send_encrypted(session, {'ok': True, 'username': user.name,
                                       'readonly': session.readonly})

    # -- download (encrypted binary frames) ------------------------------
    def _require_auth_session(self):
        session = self._get_session()
        if session is None:
            raise ApiError('No session', status=401)
        if self.config.use_auth and not session.authenticated:
            raise ApiError('Not authenticated', status=401)
        return session

    def _handle_download(self):
        session = self._require_auth_session()
        payload = self._decrypt_request(session)
        from ..libs.fileops import FileOps, FileOpError
        fo = FileOps(self.config.root_path, self.config.show_hidden,
                     self.config.exclude_items)
        try:
            ap = fo.abspath(payload.get('path', ''))
        except FileOpError as exc:
            raise ApiError(str(exc), status=400)
        if not os.path.isfile(ap):
            raise ApiError('Not a file', status=404)

        self.send_response(200)
        self.send_header('Content-Type', 'application/octet-stream')
        self.send_header('Cache-Control', 'no-store')
        self.send_header('Connection', 'close')
        self.end_headers()
        self.close_connection = True
        try:
            with open(ap, 'rb') as fh:
                while True:
                    chunk = fh.read(_DOWNLOAD_CHUNK)
                    if not chunk:
                        break
                    self.wfile.write(session.cipher.encrypt_frame(chunk))
        except BrokenPipeError:
            pass

    # -- upload (encrypted chunk envelopes) ------------------------------
    def _handle_upload(self):
        session = self._require_auth_session()
        if self.config.global_readonly or session.readonly:
            raise ApiError('Read-only mode', status=403)
        payload = self._decrypt_request(session)
        from ..libs.fileops import FileOps, FileOpError
        fo = FileOps(self.config.root_path, self.config.show_hidden,
                     self.config.exclude_items)
        name = payload.get('name', '')
        rel_dir = payload.get('path', '')
        offset = int(payload.get('offset', 0))
        is_last = bool(payload.get('last', True))
        data_b64 = payload.get('chunk', '')
        try:
            FileOps._validate_name(name)
            dest_dir = fo.abspath(rel_dir)
            if not os.path.isdir(dest_dir):
                raise FileOpError('Destination is not a directory')
            if self.config.allowed_upload_extensions is not None:
                ext = os.path.splitext(name)[1].lower().lstrip('.')
                if ext not in self.config.allowed_upload_extensions:
                    raise FileOpError('File extension not allowed')
            final = Tools.safe_join(self.config.root_path,
                                    posixpath.join(rel_dir or '', name))
            part = final + '.part'
            chunk = b64url_decode(data_b64) if data_b64 else b''
            mode = 'r+b' if (offset and os.path.exists(part)) else 'wb'
            with open(part, mode) as fh:
                fh.seek(offset)
                fh.write(chunk)
            if is_last:
                if os.path.exists(final):
                    os.remove(final)
                os.rename(part, final)
                entry = fo.stat_entry(Tools.rel_path(self.config.root_path, final))
                return self._send_encrypted(session, {'ok': True,
                                                      'result': entry})
        except FileOpError as exc:
            raise ApiError(str(exc), status=400)
        self._send_encrypted(session, {'ok': True,
                                       'result': {'received': offset + len(chunk)}})

    # -- static assets ----------------------------------------------------
    def _serve_static(self, rel):
        rel = posixpath.normpath(rel).lstrip('/')
        if rel.startswith('..') or rel == '.':
            return self._send_error_json('Not found', status=404)
        full = os.path.join(_WEB_DIR, rel)
        if not os.path.isfile(full):
            # SPA fallback: unknown non-asset path serves the app shell.
            if '.' not in os.path.basename(rel):
                full = os.path.join(_WEB_DIR, 'index.html')
            else:
                return self._send_error_json('Not found', status=404)
        ext = os.path.splitext(full)[1].lower()
        ctype = _CONTENT_TYPES.get(ext, 'application/octet-stream')
        try:
            with open(full, 'rb') as fh:
                body = fh.read()
        except OSError:
            return self._send_error_json('Not found', status=404)
        self.send_response(200)
        self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(body)))
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.send_header('Referrer-Policy', 'no-referrer')
        self.end_headers()
        self.wfile.write(body)


class FileManagerServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, config, api):
        self.config = config
        self.api = api
        super().__init__((config.host, config.port), Handler)


def serve(config):
    api = Api(config)
    httpd = FileManagerServer(config, api)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        httpd.shutdown()
        httpd.server_close()
    return 0

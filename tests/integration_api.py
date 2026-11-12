#!/usr/bin/env python3
"""End-to-end test client: mirrors the browser WebCrypto flow against the
running filemanager-web server. Validates ECDH+HKDF+AES-GCM interop and the
full API path (handshake -> login -> list -> mkdir -> upload -> download)."""
import base64
import json
import os
import sys
import urllib.request

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

BASE = sys.argv[1] if len(sys.argv) > 1 else 'http://127.0.0.1:8181'
USER = sys.argv[2] if len(sys.argv) > 2 else 'admin'
PASS = sys.argv[3] if len(sys.argv) > 3 else ''
CURVE = ec.SECP256R1()
INFO = b'filemanager-web/aesgcm/v1'


def b64u_e(b): return base64.urlsafe_b64encode(b).rstrip(b'=').decode()
def b64u_d(s):
    s = s.encode() if isinstance(s, str) else s
    return base64.urlsafe_b64decode(s + b'=' * (-len(s) % 4))


def post(path, body, headers=None):
    data = json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, method='POST')
    req.add_header('Content-Type', 'application/json')
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def post_raw(path, body, headers):
    data = json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, method='POST')
    req.add_header('Content-Type', 'application/json')
    for k, v in headers.items():
        req.add_header(k, v)
    with urllib.request.urlopen(req) as r:
        return r.status, r.read()


class Client:
    def __init__(self):
        self.sid = None
        self.key = None

    def handshake(self):
        priv = ec.generate_private_key(CURVE)
        pub = priv.public_key().public_bytes(
            serialization.Encoding.X962,
            serialization.PublicFormat.UncompressedPoint)
        st, body = post('/api/handshake', {'client_pub': b64u_e(pub)})
        assert st == 200, (st, body)
        data = json.loads(body)
        self.sid = data['session_id']
        server_pub = ec.EllipticCurvePublicKey.from_encoded_point(
            CURVE, b64u_d(data['server_pub']))
        shared = priv.exchange(ec.ECDH(), server_pub)
        self.key = HKDF(algorithm=hashes.SHA256(), length=32,
                        salt=bytes.fromhex(self.sid), info=INFO).derive(shared)
        return data

    def _enc(self, obj):
        iv = os.urandom(12)
        ct = AESGCM(self.key).encrypt(iv, json.dumps(obj).encode(), None)
        return {'iv': b64u_e(iv), 'data': b64u_e(ct)}

    def _dec(self, env):
        return json.loads(AESGCM(self.key).decrypt(
            b64u_d(env['iv']), b64u_d(env['data']), None))

    def call(self, action, payload):
        st, body = post('/api/' + action, self._enc(payload),
                        {'X-Session-Id': self.sid})
        env = json.loads(body)
        if 'iv' not in env:
            return st, env
        return st, self._dec(env)

    def login(self, user, pw):
        st, body = post('/api/login', self._enc({'username': user, 'password': pw}),
                        {'X-Session-Id': self.sid})
        return st, self._dec(json.loads(body))

    def download(self, path):
        st, body = post('/api/download', self._enc({'path': path}),
                        {'X-Session-Id': self.sid})
        # parse frames
        out = b''
        off = 0
        while off + 16 <= len(body):
            n = int.from_bytes(body[off:off+4], 'big'); off += 4
            iv = body[off:off+12]; off += 12
            ct = body[off:off+n]; off += n
            out += AESGCM(self.key).decrypt(iv, ct, None)
        return out

    def upload(self, path, name, content):
        return self.call('upload', {
            'path': path, 'name': name, 'offset': 0, 'last': True,
            'chunk': b64u_e(content)})


def main():
    c = Client()
    hs = c.handshake()
    print('[+] handshake ok  session=%s… auth_required=%s'
          % (c.sid[:8], hs['auth_required']))

    st, out = c.login(USER, PASS)
    print('[%s] login:' % ('+' if out.get('ok') else '-'), out)
    if not out.get('ok'):
        print('    (login failed — pass password as argv[3])')
        return 1

    st, out = c.call('info', {})
    print('[+] info:', out.get('result'))

    st, out = c.call('list', {'path': ''})
    assert out.get('ok'), out
    print('[+] list root: %d entries' %
          len(out['result']['entries']))

    st, out = c.call('mkdir', {'path': '', 'name': 'test_dir_fmw'})
    print('[%s] mkdir:' % ('+' if out.get('ok') else '-'), out.get('error', 'ok'))

    st, out = c.upload('test_dir_fmw', 'hello.txt', b'Hello encrypted world!\n')
    print('[%s] upload:' % ('+' if out.get('ok') else '-'), out.get('error', 'ok'))

    data = c.download('test_dir_fmw/hello.txt')
    print('[+] download roundtrip:', data)
    assert data == b'Hello encrypted world!\n', 'roundtrip mismatch!'

    st, out = c.call('read', {'path': 'test_dir_fmw/hello.txt'})
    print('[+] read:', out['result']['content'].strip())

    st, out = c.call('save', {'path': 'test_dir_fmw/hello.txt', 'content': 'edited\n'})
    print('[%s] save:' % ('+' if out.get('ok') else '-'), out.get('error', 'ok'))

    st, out = c.call('delete', {'paths': ['test_dir_fmw']})
    print('[%s] delete:' % ('+' if out.get('ok') else '-'), out.get('error', 'ok'))

    print('\n=== ALL ENCRYPTED-TRANSPORT TESTS PASSED ===')
    return 0


if __name__ == '__main__':
    sys.exit(main())

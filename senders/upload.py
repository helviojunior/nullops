#!/usr/bin/env python3
# upload.py — send a file to a nullops server through a public upload link.
#
# It speaks the same /api channel as the browser (see pkg/crypto and
# frontend/src/lib/crypto.js):
#
#   1. POST /api/handshake with an ephemeral ECDH P-256 public key.
#   2. If the server answers {encrypted:true} it negotiates an AES-256-GCM key
#      (ECDH -> HKDF-SHA256, salt = session id bytes). {encrypted:false} means
#      the server runs without TLS payload encryption and the channel is plain.
#   3. public_upload (status) to find the resume offset, then 1 MiB chunks.
#
# The TLS certificate is NOT verified, by design: these links are commonly
# served with a self-signed certificate.
#
# Requires the 'cryptography' package:  pip install cryptography
#
# Usage:
#   ./upload.py --url https://host:8443 --token <TOKEN> --file ./report.pdf
#   ./upload.py https://host:8443 <TOKEN> ./report.pdf

import argparse
import base64
import hashlib
import json
import os
import ssl
import sys
import time
import urllib.error
import urllib.request

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

HKDF_INFO = b"nullops/aesgcm/v1"
UPLOAD_CHUNK = 1024 * 1024  # 1 MiB of plaintext per request, like the browser

# TLS context that accepts any certificate (ignored for http:// URLs).
_TLS = ssl.create_default_context()
_TLS.check_hostname = False
_TLS.verify_mode = ssl.CERT_NONE


def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def b64url_decode(s: str) -> bytes:
    return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))


def http_post(url: str, body: bytes, headers: dict):
    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, context=_TLS, timeout=120) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:  # non-2xx still carries a JSON body
        return e.code, e.read()


class Client:
    def __init__(self, base: str):
        self.base = base.rstrip("/")
        self.session_id = None
        self.key = None      # None => plaintext session
        self.plain = False

    # ---- handshake: check crypto, negotiate the key if enabled -----------
    def connect(self):
        priv = ec.generate_private_key(ec.SECP256R1())
        client_pub = priv.public_key().public_bytes(
            serialization.Encoding.X962,
            serialization.PublicFormat.UncompressedPoint,
        )
        status, raw = http_post(
            self.base + "/api/handshake",
            json.dumps({"client_pub": b64url(client_pub)}).encode(),
            {"Content-Type": "application/json"},
        )
        data = json.loads(raw or b"{}")
        if data.get("code") == "secure_context":
            raise RuntimeError("server requires a secure context for the handshake")
        if status != 200:
            raise RuntimeError("handshake failed: " + str(data.get("error", status)))

        self.session_id = data["session_id"]
        if data.get("encrypted") is False:
            self.plain = True
            return "plaintext (server has no payload encryption)"

        # crypto is enabled -> negotiate the shared AES key
        server_pub = b64url_decode(data["server_pub"])
        peer = ec.EllipticCurvePublicKey.from_encoded_point(ec.SECP256R1(), server_pub)
        shared = priv.exchange(ec.ECDH(), peer)
        self.key = HKDF(
            algorithm=hashes.SHA256(),
            length=32,
            salt=bytes.fromhex(self.session_id),
            info=HKDF_INFO,
        ).derive(shared)
        return "encrypted (AES-256-GCM negotiated)"

    # ---- envelope --------------------------------------------------------
    def _seal(self, plaintext: bytes) -> dict:
        iv = os.urandom(12)
        ct = AESGCM(self.key).encrypt(iv, plaintext, None)
        return {"iv": b64url(iv), "data": b64url(ct)}

    def _open(self, env: dict) -> bytes:
        return AESGCM(self.key).decrypt(b64url_decode(env["iv"]), b64url_decode(env["data"]), None)

    def post(self, action: str, payload: dict) -> dict:
        plain = json.dumps(payload).encode()
        body = plain if self.plain else json.dumps(self._seal(plain)).encode()
        status, raw = http_post(
            self.base + "/api/" + action,
            body,
            {"Content-Type": "application/json", "X-Session-Id": self.session_id},
        )
        data = json.loads(raw or b"{}")
        if status != 200 or (not self.plain and "iv" not in data):
            raise RuntimeError(data.get("error", "request failed (HTTP %d)" % status))
        return data if self.plain else json.loads(self._open(data))

    def call(self, action: str, payload: dict):
        out = self.post(action, payload)
        if out.get("ok") is False:
            raise RuntimeError(out.get("error", "error"))
        return out.get("result", out)

    def call_retry(self, action: str, payload: dict, attempts=4):
        for i in range(attempts):
            try:
                return self.call(action, payload)
            except RuntimeError:
                if i == attempts - 1:
                    raise
                time.sleep(1.0 * (2 ** i))


def upload_id(name: str, size: int, mtime: int) -> str:
    # Stable id for this file so a re-run resumes the partial upload.
    return hashlib.sha256(f"{name}|{size}|{mtime}".encode()).hexdigest()


def main():
    ap = argparse.ArgumentParser(description="Upload a file to a nullops public upload link.")
    ap.add_argument("url", nargs="?", help="server base URL, e.g. https://host:8443")
    ap.add_argument("token", nargs="?", help="upload token")
    ap.add_argument("file", nargs="?", help="path of the file to send")
    ap.add_argument("-u", "--url", dest="url_opt", help="server base URL")
    ap.add_argument("-t", "--token", dest="token_opt", help="upload token")
    ap.add_argument("-f", "--file", dest="file_opt", help="path of the file to send")
    args = ap.parse_args()

    url = args.url_opt or args.url
    token = args.token_opt or args.token
    path = args.file_opt or args.file
    if not (url and token and path):
        ap.error("url, token and file are all required")
    if not os.path.isfile(path):
        ap.error("not a file: " + path)

    name = os.path.basename(path)
    size = os.path.getsize(path)
    mtime = int(os.path.getmtime(path))
    fields = {"token": token, "name": name, "upload_id": upload_id(name, size, mtime), "size": size}

    client = Client(url)
    print("[*] connecting to %s" % client.base, file=sys.stderr)
    print("[*] %s" % client.connect(), file=sys.stderr)

    status = client.call("public_upload", {**fields, "status": True})
    offset = min(int(status.get("received") or 0), size)
    if offset:
        print("[*] resuming at %d/%d bytes" % (offset, size), file=sys.stderr)

    with open(path, "rb") as f:
        while True:
            f.seek(offset)
            chunk = f.read(UPLOAD_CHUNK)
            end = offset + len(chunk)
            last = end >= size
            res = client.call_retry(
                "public_upload",
                {**fields, "offset": offset, "last": last, "chunk": b64url(chunk)},
            )
            offset = end
            pct = 100.0 * offset / size if size else 100.0
            print("\r[*] %d/%d bytes (%.1f%%)" % (offset, size, pct), end="", file=sys.stderr)
            if last:
                print("", file=sys.stderr)
                print("[+] stored as %s (%d bytes)" % (res.get("name", name), res.get("size", size)))
                return


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError) as e:
        print("\n[!] %s" % e, file=sys.stderr)
        sys.exit(1)

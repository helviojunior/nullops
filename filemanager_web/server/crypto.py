# -*- coding: utf-8 -*-
"""Transport encryption for the /api channel.

Design (matches the browser WebCrypto implementation in web/js/crypto.js):

  Handshake (per session, keys live only in memory):
    1. Client generates an ephemeral ECDH P-256 key pair and sends its public
       key (raw uncompressed point, base64url) to POST /api/handshake.
    2. Server generates its own ephemeral P-256 key pair, computes the ECDH
       shared secret, and derives a 256-bit AES-GCM key via HKDF-SHA256 using
       salt = the random session id bytes and info = b"filemanager-web/aesgcm/v1".
    3. Server returns {session_id, server_pub (base64url raw point)}.
    4. Client derives the same key. The AES key is never transmitted.

  Per-request envelope (both directions):
    {"iv": base64url(12 bytes), "data": base64url(ciphertext || gcm_tag)}
    AES-256-GCM. WebCrypto appends the 16-byte tag to the ciphertext, which is
    exactly what cryptography's AESGCM expects, so the two interoperate.

This module depends only on the `cryptography` package (the single allowed
third-party dependency) plus the stdlib.
"""
import base64
import json
import os

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

_CURVE = ec.SECP256R1()
_HKDF_INFO = b'filemanager-web/aesgcm/v1'
_IV_LEN = 12


def b64url_encode(data):
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode('ascii')


def b64url_decode(text):
    if isinstance(text, str):
        text = text.encode('ascii')
    pad = b'=' * (-len(text) % 4)
    return base64.urlsafe_b64decode(text + pad)


def derive_key(private_key, peer_public_bytes, salt):
    """Derive the shared AES-256 key from an ECDH exchange."""
    peer_public = ec.EllipticCurvePublicKey.from_encoded_point(
        _CURVE, peer_public_bytes,
    )
    shared = private_key.exchange(ec.ECDH(), peer_public)
    return HKDF(
        algorithm=hashes.SHA256(),
        length=32,
        salt=salt,
        info=_HKDF_INFO,
    ).derive(shared)


def generate_keypair():
    private_key = ec.generate_private_key(_CURVE)
    public_bytes = private_key.public_key().public_bytes(
        encoding=serialization.Encoding.X962,
        format=serialization.PublicFormat.UncompressedPoint,
    )
    return private_key, public_bytes


class SessionCipher(object):
    """Holds a derived AES-GCM key and encrypts/decrypts JSON envelopes."""

    __slots__ = ('_aesgcm',)

    def __init__(self, key):
        self._aesgcm = AESGCM(key)

    def encrypt_json(self, obj):
        plaintext = json.dumps(obj, separators=(',', ':')).encode('utf-8')
        return self.encrypt_bytes(plaintext)

    def encrypt_bytes(self, plaintext):
        iv = os.urandom(_IV_LEN)
        ct = self._aesgcm.encrypt(iv, plaintext, None)
        return {'iv': b64url_encode(iv), 'data': b64url_encode(ct)}

    def decrypt_to_bytes(self, envelope):
        iv = b64url_decode(envelope['iv'])
        ct = b64url_decode(envelope['data'])
        return self._aesgcm.decrypt(iv, ct, None)

    def decrypt_json(self, envelope):
        raw = self.decrypt_to_bytes(envelope)
        if not raw:
            return {}
        return json.loads(raw.decode('utf-8'))

    # -- binary stream framing (for download/upload of large files) -------
    # Frame layout: [4-byte big-endian length N][12-byte IV][N bytes ct||tag]
    def encrypt_frame(self, plaintext):
        iv = os.urandom(_IV_LEN)
        ct = self._aesgcm.encrypt(iv, plaintext, None)
        return len(ct).to_bytes(4, 'big') + iv + ct

    def decrypt_frame(self, iv, ct):
        return self._aesgcm.decrypt(iv, ct, None)

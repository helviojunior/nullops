# -*- coding: utf-8 -*-
"""User store and password hashing.

Hashing uses hashlib.scrypt (stdlib only). Stored format:

    scrypt$<n>$<r>$<p>$<salt_hex>$<dk_hex>

Credentials may be:
  * passed explicitly (--user / --pass),
  * auto-generated (random password printed once at startup),
  * loaded from a JSON config: {"users": {"name": "<hash-or-plain>"}}.

A user may be flagged read-only. Verification is constant-time.
"""
import hashlib
import hmac
import secrets
import string


_SCRYPT_N = 2 ** 14
_SCRYPT_R = 8
_SCRYPT_P = 1
_DKLEN = 32


def hash_password(password, salt=None):
    if isinstance(password, str):
        password = password.encode('utf-8')
    if salt is None:
        salt = secrets.token_bytes(16)
    dk = hashlib.scrypt(
        password, salt=salt,
        n=_SCRYPT_N, r=_SCRYPT_R, p=_SCRYPT_P, dklen=_DKLEN,
        maxmem=0,
    )
    return 'scrypt$%d$%d$%d$%s$%s' % (
        _SCRYPT_N, _SCRYPT_R, _SCRYPT_P, salt.hex(), dk.hex(),
    )


def verify_password(password, stored):
    if not stored:
        return False
    if isinstance(password, str):
        password = password.encode('utf-8')
    try:
        scheme, n, r, p, salt_hex, dk_hex = stored.split('$')
        if scheme != 'scrypt':
            return False
        salt = bytes.fromhex(salt_hex)
        expected = bytes.fromhex(dk_hex)
        dk = hashlib.scrypt(
            password, salt=salt,
            n=int(n), r=int(r), p=int(p), dklen=len(expected),
            maxmem=0,
        )
        return hmac.compare_digest(dk, expected)
    except (ValueError, TypeError):
        return False


def generate_password(length=16):
    alphabet = string.ascii_letters + string.digits
    return ''.join(secrets.choice(alphabet) for _ in range(length))


class User(object):
    __slots__ = ('name', 'password_hash', 'readonly', 'home')

    def __init__(self, name, password_hash, readonly=False, home=None):
        self.name = name
        self.password_hash = password_hash
        self.readonly = bool(readonly)
        self.home = home  # optional per-user sub-root (relative to server root)


class UserStore(object):
    """In-memory user registry."""

    def __init__(self):
        self._users = {}

    def add(self, name, password=None, password_hash=None,
            readonly=False, home=None):
        if password_hash is None:
            if password is None:
                raise ValueError('password or password_hash required')
            password_hash = hash_password(password)
        elif not str(password_hash).startswith('scrypt$'):
            # treat a non-hashed value as a plaintext password for convenience
            password_hash = hash_password(password_hash)
        self._users[name] = User(name, password_hash, readonly, home)
        return self._users[name]

    def verify(self, name, password):
        user = self._users.get(name)
        if user is None:
            # Perform a dummy hash to reduce user-enumeration timing signal.
            hash_password(password or '')
            return None
        if verify_password(password, user.password_hash):
            return user
        return None

    def get(self, name):
        return self._users.get(name)

    def __len__(self):
        return len(self._users)

    def names(self):
        return list(self._users.keys())

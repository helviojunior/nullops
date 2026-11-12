# -*- coding: utf-8 -*-
import unittest

from filemanager_web.libs.auth import (hash_password, verify_password,
                                       UserStore, generate_password)
from filemanager_web.server.crypto import generate_keypair, derive_key, SessionCipher


class AuthTest(unittest.TestCase):
    def test_hash_verify(self):
        h = hash_password('s3cret')
        self.assertTrue(verify_password('s3cret', h))
        self.assertFalse(verify_password('wrong', h))

    def test_store(self):
        store = UserStore()
        store.add('bob', password='pw', readonly=True)
        user = store.verify('bob', 'pw')
        self.assertIsNotNone(user)
        self.assertTrue(user.readonly)
        self.assertIsNone(store.verify('bob', 'nope'))
        self.assertIsNone(store.verify('ghost', 'pw'))

    def test_generate_password(self):
        self.assertEqual(len(generate_password(20)), 20)


class CryptoTest(unittest.TestCase):
    def test_ecdh_derives_same_key(self):
        # simulate both sides of the handshake
        salt = b'0123456789abcdef'
        a_priv, a_pub = generate_keypair()
        b_priv, b_pub = generate_keypair()
        key_a = derive_key(a_priv, b_pub, salt)
        key_b = derive_key(b_priv, a_pub, salt)
        self.assertEqual(key_a, key_b)

    def test_envelope_roundtrip(self):
        _priv, _pub = generate_keypair()
        key = derive_key(_priv, _pub, b'saltsaltsaltsalt')
        cipher = SessionCipher(key)
        env = cipher.encrypt_json({'hello': 'world', 'n': 42})
        self.assertIn('iv', env)
        self.assertIn('data', env)
        out = cipher.decrypt_json(env)
        self.assertEqual(out, {'hello': 'world', 'n': 42})

    def test_frame_roundtrip(self):
        _priv, _pub = generate_keypair()
        key = derive_key(_priv, _pub, b'saltsaltsaltsalt')
        cipher = SessionCipher(key)
        frame = cipher.encrypt_frame(b'binary\x00data')
        n = int.from_bytes(frame[:4], 'big')
        iv = frame[4:16]
        ct = frame[16:16 + n]
        self.assertEqual(cipher.decrypt_frame(iv, ct), b'binary\x00data')


if __name__ == '__main__':
    unittest.main()

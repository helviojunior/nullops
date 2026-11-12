/*
 * Transport encryption for the /api channel (browser side).
 * Mirrors filemanager_web/server/crypto.py exactly:
 *   ECDH P-256  ->  HKDF-SHA256(salt = session_id bytes,
 *                               info = "filemanager-web/aesgcm/v1")  ->  AES-256-GCM
 * The AES key never leaves the browser; it lives only in this CryptoKey object.
 */
(function (global) {
  'use strict';

  var HKDF_INFO = new TextEncoder().encode('filemanager-web/aesgcm/v1');

  // ---- base64url helpers -------------------------------------------------
  function b64urlEncode(bytes) {
    var bin = '';
    var arr = new Uint8Array(bytes);
    for (var i = 0; i < arr.length; i++) bin += String.fromCharCode(arr[i]);
    return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }

  function b64urlDecode(str) {
    str = str.replace(/-/g, '+').replace(/_/g, '/');
    while (str.length % 4) str += '=';
    var bin = atob(str);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  function hexToBytes(hex) {
    var out = new Uint8Array(hex.length / 2);
    for (var i = 0; i < out.length; i++) {
      out[i] = parseInt(hex.substr(i * 2, 2), 16);
    }
    return out;
  }

  // ---- session cipher ----------------------------------------------------
  function SessionCrypto() {
    this.sessionId = null;
    this.aesKey = null;
  }

  SessionCrypto.prototype.handshake = async function () {
    var pair = await crypto.subtle.generateKey(
      { name: 'ECDH', namedCurve: 'P-256' }, false, ['deriveBits']);
    var clientPubRaw = await crypto.subtle.exportKey('raw', pair.publicKey);

    var resp = await fetch('/api/handshake', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ client_pub: b64urlEncode(clientPubRaw) })
    });
    if (!resp.ok) throw new Error('Handshake failed');
    var data = await resp.json();

    this.sessionId = data.session_id;
    var serverPubRaw = b64urlDecode(data.server_pub);
    var serverPub = await crypto.subtle.importKey(
      'raw', serverPubRaw, { name: 'ECDH', namedCurve: 'P-256' }, false, []);

    var sharedBits = await crypto.subtle.deriveBits(
      { name: 'ECDH', public: serverPub }, pair.privateKey, 256);
    var hkdfKey = await crypto.subtle.importKey(
      'raw', sharedBits, 'HKDF', false, ['deriveKey']);
    this.aesKey = await crypto.subtle.deriveKey(
      {
        name: 'HKDF', hash: 'SHA-256',
        salt: hexToBytes(this.sessionId), info: HKDF_INFO
      },
      hkdfKey,
      { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);

    return { authRequired: !!data.auth_required };
  };

  SessionCrypto.prototype.encryptJson = async function (obj) {
    var iv = crypto.getRandomValues(new Uint8Array(12));
    var pt = new TextEncoder().encode(JSON.stringify(obj));
    var ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: iv }, this.aesKey, pt);
    return { iv: b64urlEncode(iv), data: b64urlEncode(ct) };
  };

  SessionCrypto.prototype.decryptJson = async function (envelope) {
    var iv = b64urlDecode(envelope.iv);
    var ct = b64urlDecode(envelope.data);
    var pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: iv }, this.aesKey, ct);
    return JSON.parse(new TextDecoder().decode(pt));
  };

  SessionCrypto.prototype.encryptBytes = async function (bytes) {
    var iv = crypto.getRandomValues(new Uint8Array(12));
    var ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: iv }, this.aesKey, bytes);
    return { iv: b64urlEncode(iv), data: b64urlEncode(ct) };
  };

  // Decrypt a stream of length-prefixed frames:
  //   [4-byte big-endian len][12-byte iv][len bytes ct||tag]
  SessionCrypto.prototype.decryptFrames = async function (arrayBuffer) {
    var view = new DataView(arrayBuffer);
    var bytes = new Uint8Array(arrayBuffer);
    var offset = 0;
    var parts = [];
    while (offset + 16 <= bytes.length) {
      var n = view.getUint32(offset, false);
      offset += 4;
      var iv = bytes.subarray(offset, offset + 12);
      offset += 12;
      var ct = bytes.subarray(offset, offset + n);
      offset += n;
      var pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: iv }, this.aesKey, ct);
      parts.push(new Uint8Array(pt));
    }
    return new Blob(parts);
  };

  global.SessionCrypto = SessionCrypto;
  global.b64url = { encode: b64urlEncode, decode: b64urlDecode };
})(window);

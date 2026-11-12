/*
 * Encrypted API client. All /api calls (except the handshake inside
 * SessionCrypto) ride through AES-GCM envelopes keyed by the session.
 */
(function (global) {
  'use strict';

  function ApiClient() {
    this.crypto = new SessionCrypto();
    this.ready = false;
    this.authRequired = true;
    this.user = null;
    this.readonly = false;
  }

  ApiClient.prototype._headers = function () {
    return {
      'Content-Type': 'application/json',
      'X-Session-Id': this.crypto.sessionId
    };
  };

  ApiClient.prototype.connect = async function () {
    var info = await this.crypto.handshake();
    this.authRequired = info.authRequired;
    this.ready = true;
    return info;
  };

  // Encrypted JSON action -> decrypted result (throws on {ok:false}).
  ApiClient.prototype.call = async function (action, payload) {
    var envelope = await this.crypto.encryptJson(payload || {});
    var resp = await fetch('/api/' + action, {
      method: 'POST',
      headers: this._headers(),
      body: JSON.stringify(envelope)
    });
    var data = await resp.json();
    // Server errors before decryption come back as plaintext {ok:false,error}.
    if (data && data.ok === false && data.error && !data.iv) {
      throw new ApiError(data.error, resp.status);
    }
    var out = await this.crypto.decryptJson(data);
    if (out.ok === false) throw new ApiError(out.error || 'Error', resp.status);
    return out.result !== undefined ? out.result : out;
  };

  ApiClient.prototype.login = async function (username, password) {
    var envelope = await this.crypto.encryptJson({
      username: username, password: password
    });
    var resp = await fetch('/api/login', {
      method: 'POST', headers: this._headers(), body: JSON.stringify(envelope)
    });
    var out = await this.crypto.decryptJson(await resp.json());
    if (out.ok) {
      this.user = out.username;
      this.readonly = !!out.readonly;
    }
    return out;
  };

  ApiClient.prototype.logout = async function () {
    try { await this.call('logout', {}); } catch (e) { /* ignore */ }
  };

  // Download: encrypted request {path} -> encrypted binary frames -> Blob.
  ApiClient.prototype.download = async function (path) {
    var envelope = await this.crypto.encryptJson({ path: path });
    var resp = await fetch('/api/download', {
      method: 'POST', headers: this._headers(), body: JSON.stringify(envelope)
    });
    if (!resp.ok) {
      var err = await resp.json().catch(function () { return {}; });
      throw new ApiError(err.error || 'Download failed', resp.status);
    }
    var buf = await resp.arrayBuffer();
    return await this.crypto.decryptFrames(buf);
  };

  // Upload one File in encrypted chunks. onProgress(sent, total).
  ApiClient.prototype.upload = async function (path, file, onProgress) {
    var CHUNK = 1024 * 1024; // 1 MiB plaintext chunks
    var offset = 0;
    while (offset < file.size || (file.size === 0 && offset === 0)) {
      var end = Math.min(offset + CHUNK, file.size);
      var slice = file.slice(offset, end);
      var bytes = new Uint8Array(await slice.arrayBuffer());
      var last = end >= file.size;
      var result = await this.call('upload', {
        path: path,
        name: file.name,
        offset: offset,
        last: last,
        chunk: b64url.encode(bytes)
      });
      offset = end;
      if (onProgress) onProgress(offset, file.size);
      if (last) return result;
      if (file.size === 0) break;
    }
    return null;
  };

  function ApiError(message, status) {
    this.message = message;
    this.status = status;
    this.name = 'ApiError';
  }
  ApiError.prototype = Object.create(Error.prototype);

  global.ApiClient = ApiClient;
  global.ApiError = ApiError;
})(window);

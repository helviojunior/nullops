# filemanager-web

A modern, stand-alone, **encrypted** web file manager — a from-scratch rewrite
and improvement of [tinyfilemanager](https://github.com/prasathmani/tinyfilemanager),
powered only by the Python standard-library `http.server`.

Run it like you run `python3 -m http.server`, but get a full file-management UI
with an **end-to-end encrypted `/api` channel**.

```bash
pip install filemanager-web
filemanager-web                 # serve ./ on http://127.0.0.1:8181
filemanager-web -r /data -p 9000
```

On start it prints the URL and an auto-generated password (Jupyter-token style):

```
  filemanager-web v0.0.0
  serving /data

  URL:  http://127.0.0.1:8181/

  user: admin
  pass: 7Qx2fK9pLmA3bVnZ   (auto-generated)

  transport: ECDH P-256 + AES-256-GCM per session (keys in memory only)
```

## Highlights

- **Stand-alone.** Only the Python stdlib `http.server` for the web server. The
  single third-party dependency is `cryptography`, used exclusively for the
  encrypted transport. No Django, no database, no framework.
- **Encrypted `/api` transport.** Every API payload is sealed in AES-256-GCM.
  The key is derived per session with an ephemeral **ECDH P-256** handshake
  (HKDF-SHA256) and **lives only in memory** — never written to disk, never
  reused across sessions, never sent over the wire.
- **Clean front/back split.** The browser talks to a JSON `/api/...` surface;
  static assets are served separately. The UI is a small vanilla-JS SPA.
- **CLI-first.** Credentials are passed with `--user/--pass` or auto-generated
  and printed once at startup.

## Features (parity with tinyfilemanager)

- Directory browser with breadcrumb navigation, sizes, dates and permissions
- Upload (drag-and-drop, multi-file, chunked) and download — both encrypted
- Create file / create folder, rename, delete (single + batch)
- Copy / move, `chmod`
- Built-in text/code editor (read + save)
- Zip / tar archive creation and extraction (with path-traversal guards)
- Recursive search
- Read-only mode (`--readonly`) and optional no-auth mode (`--no-auth`)
- Light/dark theme, responsive layout

## Usage

```
filemanager-web [-r DIR] [-b ADDR] [-p PORT]
                [--user NAME] [--pass PASS] [--no-auth] [--readonly]
                [--session-ttl SEC] [--show-hidden] [--title TEXT]
                [--exclude GLOB] [-v] [-q]
```

| Option | Description |
| --- | --- |
| `-r, --root DIR` | Directory to serve (default: current directory) |
| `-b, --bind ADDR` | Bind address (default: `127.0.0.1`) |
| `-p, --port PORT` | TCP port (default: `8181`) |
| `--user` / `--pass` | Credentials (password auto-generated if omitted) |
| `--no-auth` | Disable authentication |
| `--readonly` | Serve read-only |
| `--show-hidden` | Show dotfiles |
| `--exclude GLOB` | Hide entries matching a glob (repeatable) |

Also runnable as a module: `python -m filemanager_web`.

## Security model

- The AES key is derived via ECDH + HKDF **per session** and held only in RAM.
- Path traversal and symlink escapes are blocked; every path is resolved and
  contained under the served root.
- Archive extraction is guarded against `../` members (zip-slip).
- WebCrypto requires a **secure context**: use `localhost` or serve behind
  HTTPS when binding to a public address.

> Like the original, this tool is meant for trusted/administrative use, not as a
> public multi-tenant service.

## Development & testing

```bash
python -m venv .venv && . .venv/bin/activate
pip install -e ".[dev]"
python -m playwright install chromium

# unit tests (fileops, crypto, auth)
python -m unittest discover -s tests

# integration tests (need a running server)
filemanager-web -r /tmp/fmw_test --user admin --pass testpass123 &
python tests/integration_api.py      http://127.0.0.1:8181 admin testpass123
python tests/integration_browser.py  # headless Chrome; 0 JS errors expected
```

The browser suite drives the real UI in headless Chrome and asserts that **no
JavaScript console error, uncaught page error, or server 5xx** occurs across
every flow (login, CRUD, editor, upload/download, search, batch ops, logout).

## License

GPL-3.0 © Helvio Junior (M4v3r1ck)

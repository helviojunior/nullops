# TODO — Web File Manager (`fmweb`)

Requirements, what is already implemented and the roadmap of the Go port.
The original Python implementation is preserved in the `python` branch.

Legend: `[x]` done · `[ ]` pending · `[~]` partial

---

## 1. Project requirements (Go port)

- [x] Python code moved to the `python` branch; `main` cleaned for the Go port
- [x] Same layout as [enumdns](https://github.com/helviojunior/enumdns): `main.go`, `cmd/`, `internal/`, `pkg/`, `Makefile`
- [x] Code split in two: **frontend** (`frontend/`) and **backend** (Go)
- [x] Modern, pure-JavaScript front-end in the `sec_base_system` style (React 19 + Tailwind, `.jsx`, no TypeScript), built with Vite
- [x] Single executable with 100% of the HTML/JS/CSS/images embedded (one `.exe` is enough to run the system)
- [x] Embedded files are **not** extracted to disk: decoded once into RAM and served from memory
- [x] Embedded files are obfuscated: zip + AES-256-GCM blob (no HTML/JS/CSS strings in the binary)
- [x] Three subcommands: `create-tls`, `add-user`, `web` (plus `version`)
- [x] Build order: front-end first (html/css/js/images), then `go build`
- [x] Pure-Go web server (`net/http`, no external web framework)
- [x] User files (TLS cert/key, users) stored in `./fmweb` (`--data-dir` to change it)
- [x] Binary name `fmweb`, project name "Web File Manager"
- [x] Makefile with local build and cross-compile (Windows / Linux / macOS; amd64 / 386 / arm64)
- [x] Front-end build through Docker (`FRONTEND_BUILDER=local` uses the host's npm)

## 2. Implemented

### Backend / security
- [x] Encrypted `/api` channel: ECDH P-256 handshake → HKDF-SHA256 → AES-256-GCM, per-session keys kept only in memory
- [x] Wire protocol compatible with the Python version (validated with its integration client)
- [x] HTTPS by default; self-signed ECDSA certificate generated on first run when missing
- [x] `create-tls`: generate (`--host`, `--days`, `--force`) or import (`--cert/--key`) a certificate
- [x] `add-user`: scrypt hashes (same format as the Python version), prompt / `-p` / `--generate`, `--readonly`, `--update`
- [x] First run without users: creates `admin` with a random password, printed only once
- [x] Login throttling per IP (5 failures → 5 min lockout); constant-time password check
- [x] Path traversal and symlink escape blocked; delete/rename act on the link, never on its target
- [x] Zip-slip protection and size cap on extraction; symlinks inside archives are ignored
- [x] Security headers (CSP without inline scripts, `X-Frame-Options`, `nosniff`, `no-referrer`)
- [x] Global read-only mode (`--readonly`) and per-user read-only accounts
- [x] `--no-tls` disables the payload encryption too (no WebCrypto): console warning + `[y/N]` confirmation
      (`-y/--yes` for non-interactive use), red banner in the UI; a TLS server refuses unencrypted clients
- [x] Console log: page access (`/`), login (user, IP, User-Agent), logout, and every file operation
      (upload, download, open, edit, create, rename, delete, copy, move, chmod, zip/tar, extract) with failures
- [x] Verbosity levels (`-q`, default, `-v`, `-vv`): TLS handshake / HTTP server errors only show at debug (`-v`)
- [x] Upload extension allow-list (`--allow-ext`), hidden files (`--show-hidden`), exclusion globs (`--exclude`)

### File operations
- [x] Directory listing with breadcrumb, size, date, permissions
- [x] **Create new file** and create new folder
- [x] Rename, delete (single and batch), copy / move (batch), `chmod`
- [x] Encrypted chunked upload (button and drag-and-drop, multiple files, progress)
- [x] Encrypted streamed download with progress
- [x] Zip and tar.gz creation; zip / tar / tar.gz extraction (optionally into a subfolder)
- [x] Recursive search by name

### Web interface
- [x] **Edit text files directly in the web UI**: txt, md, log, json, yaml, xml, html, css, js, go, py, sh,
      **ps1 / psm1 / psd1, vbs, cmd, bat, reg**, sql, ini, conf and others (5 MiB limit, Ctrl+S to save, line numbers)
- [x] Editor on its own route (`/edit/<path>`), with a "discard changes?" confirmation when leaving
- [x] EN / PT-BR (EN is the default and the fallback), language remembered per browser
- [x] Light / dark theme
- [x] System modals only (no `alert` / `confirm` / `prompt` from the browser) and toasts
- [x] Clear error when the page is not in a secure context (WebCrypto requires https or localhost)

### Quality
- [x] Go unit tests: traversal / symlink, CRUD, zip-slip, chunked upload, scrypt (Python-compatible hash), crypto, bundle
- [x] Interface validated end to end in the browser (login, listing, editing, creation, i18n, no console errors)

## 3. Roadmap

### Preview in the front-end
- [ ] **Preview images** (png, jpg, gif, webp, svg, bmp, ico) directly in the interface
- [ ] **Preview PDF** directly in the interface (blob URL from the encrypted download; check the CSP for `object-src` / `frame-src blob:`)
- [ ] **Preview small text files** (read-only view, without opening the editor)
- [ ] Preview on its own route (`/view/<path>`), in line with the "detail in its own window" rule
- [ ] Previous / next navigation between files in the same folder
- [ ] Size limit for preview (avoid loading huge files into the browser's memory)

### Editor
- [ ] Syntax highlighting (e.g. CodeMirror 6) keeping the CSP without `unsafe-eval`
- [ ] Preserve the original line endings (CRLF for `.bat` / `.cmd` / `.ps1`) and encoding (UTF-16 / BOM in Windows scripts)
- [ ] Detect when the file changed on disk since it was opened (conflict on save)

### Operations
- [ ] Download a folder / multiple selection as zip (streamed, without a temporary file)
- [ ] Resumable upload (resume after a dropped connection) and a per-file size limit
- [ ] Copy / move with a folder picker (instead of typing the path)
- [ ] Sort by column and filter by type

### Users and security
- [ ] Per-user home directory (restrict each user to a subfolder)
- [ ] `del-user` / `list-users` subcommands
- [ ] MFA (TOTP) at login
- [ ] Audit log also in a file / JSON format, with timestamps (today: console only)
- [ ] Real client IP behind a reverse proxy (`X-Forwarded-For` from trusted proxies)
- [ ] ACME / Let's Encrypt in `create-tls`

### Build / distribution
- [ ] CI (GitHub Actions) with `make all`, tests and release with checksums
- [ ] Per-build obfuscation key (pepper via `-ldflags`)
- [ ] Optional obfuscation of the JS shipped to the browser (javascript-obfuscator)
- [ ] Unit tests for `pkg/server` (HTTP handlers) and front-end tests

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
- [x] Encrypted `/api` channel: ECDH P-256 handshake → HKDF-SHA256 → AES-256-GCM, one key per session
- [x] Sessions persisted in `./fmweb/sessions.db` (pure-Go SQLite), valid for 12h (`--session-ttl`), surviving
      page reloads and server restarts: id stored only as SHA-256, AES key wrapped with a key derived from the id;
      browser keeps a non-extractable `CryptoKey` in IndexedDB; logout deletes the session
- [x] Account re-checked on every request (removed user loses the session; read-only flag changes apply)
- [x] Wire protocol compatible with the Python version (validated with its integration client)
- [x] HTTPS by default; self-signed ECDSA certificate generated on first run when missing
- [x] `create-tls`: generate (`--host`, `--days`, `--force`) or import (`--cert/--key`) a certificate
- [x] `add-user`: scrypt hashes (same format as the Python version), prompt / `-p` / `--generate`, `--readonly`, `--update`
- [x] First run without users: creates `admin` with a random password, printed only once
- [x] Login in steps over the encrypted channel: **user → [MFA] → password**. An unknown user always gets the
      MFA step (as if it existed with MFA) and the attempt is logged ("login attempt with unknown user"); its
      MFA step always fails. Note: a real user *without* MFA goes straight to the password step, so only the
      "MFA" answer is ambiguous — enable MFA for every user to hide account existence completely
- [x] **Optional MFA (TOTP, RFC 6238)**: enabled per user by the user in the web UI (Account → QR code + confirm
      code; disabling requires a current code) or by the admin with `add-user --mfa` (QR code printed in the
      terminal) / `--no-mfa`; ±30 s clock skew, a code cannot be reused (anti-replay)
- [x] Login throttling per IP (5 failures → 5 min lockout, MFA and password failures count); constant-time checks
- [x] `list-users`, `del-user` (also revokes the user's sessions), `add-user --update` keeps the password unless
      `-p` / `--generate` / `--set-password`
- [x] `sessions` / `sessions revoke <id-prefix> | --user <name> | --all`; a running server drops a revoked
      session within ~5 s
- [x] `users.json` reloaded automatically when it changes on disk (no restart after `add-user` / `del-user`)
- [x] Audit log to a JSON-lines file (`--audit-log FILE`, RFC 3339 timestamps); console lines have timestamps
- [x] Real client IP behind a reverse proxy: `--trusted-proxy <ip/cidr>` (X-Forwarded-For walked from the right,
      only trusted hops skipped; X-Real-IP fallback); untrusted clients cannot spoof it
- [x] ACME / Let's Encrypt in `create-tls --acme --domain …` (HTTP-01 on a temporary server, `--acme-email`,
      `--acme-staging`, `--acme-http-addr`, `--acme-directory`; account key kept; renew = run again with `--force`)
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
- [x] Encrypted chunked upload (button and drag-and-drop, multiple files, progress), **resumable**: a dropped
      connection is retried with backoff, and uploading the same file again continues from the hidden
      `.fmweb-upload-<id>.part`; per-file size limit `--max-upload 2G`
- [x] Encrypted streamed download with progress
- [x] Download a folder or a multiple selection as a zip built on the fly (streamed, no temporary file)
- [x] Copy / move with a folder picker (tree), and drag and drop of files onto folders in the table or in the
      side panel (move; hold Ctrl/⌥ to copy)
- [x] Sort by name / size / date (folders first, natural order) and filter by type
      (folders, images, documents, archives, code/scripts, audio/video, other); choice remembered
- [x] Zip and tar.gz creation; zip / tar / tar.gz extraction (optionally into a subfolder)
- [x] Recursive search by name

### Web interface
- [x] **Edit text files directly in the web UI**: txt, md, log, json, yaml, xml, html, css, js, go, py, sh,
      **ps1 / psm1 / psd1, vbs, cmd, bat, reg**, sql, ini, conf and others (5 MiB limit, Ctrl+S to save, line numbers)
- [x] Clicking a file sniffs the MIME type of its first 4096 bytes (`probe` action): `text/*` that is valid
      UTF-8 opens in the viewer/editor, anything else is downloaded; the extension list is only the fallback
      (a text file with an unknown extension is editable, a binary named `.txt` is downloaded)
- [x] Left side panel with the folder hierarchy: loaded on demand, auto-expands to the folder being browsed,
      updated by every listing (new/renamed/moved folders show up without reload); **resizable** by dragging its
      edge (180–600 px, double-click resets, arrow keys when focused) and **collapsible** (header button or panel
      button); width and open/closed state remembered per browser
- [x] File table columns adapt to the space left by the panel
- [x] Editor on its own route (`/edit/<path>`), with a "discard changes?" confirmation when leaving
- [x] Syntax highlighting with CodeMirror 6 (JS/TS, JSON, HTML, CSS, XML, Python, Markdown, SQL, YAML, Go,
      shell, PowerShell, VBScript, TOML, INI/properties/BAT, Dockerfile, C/C++/Java/C#/Kotlin, Rust, Ruby, Lua,
      Perl); languages loaded on demand, editor loaded lazily; CSP still without `unsafe-eval`
- [x] Original encoding and line endings preserved (UTF-8, UTF-8 BOM, UTF-16 LE/BE with BOM; LF / CRLF) —
      Windows scripts in UTF-16 are now editable
- [x] Conflict detection on save (file changed on disk since it was opened): overwrite / reload from disk / cancel
- [x] **Preview** on its own route (`/view/<path>`): images (png, jpg, gif, webp, svg, bmp, ico, avif), PDF
      (browser viewer on a blob of the decrypted download) and text (read-only), with previous / next in the
      folder (← → keys); size limits (50 MiB media, 1 MiB text) offer a download instead
- [x] EN / PT-BR (EN is the default and the fallback), language remembered per browser
- [x] Light / dark theme
- [x] System modals only (no `alert` / `confirm` / `prompt` from the browser) and toasts
- [x] Clear error when the page is not in a secure context (WebCrypto requires https or localhost)

### Quality
- [x] Go unit tests: traversal / symlink, CRUD, zip-slip, resumable upload, encodings / conflicts, streamed zip,
      MIME probe, scrypt (Python-compatible hash), TOTP (RFC 6238 vector, replay), users reload, sessions,
      crypto, bundle / per-build key, ACME option handling
- [x] HTTP tests of `pkg/server` (httptest + a Go client of the encrypted protocol): login steps and deception,
      MFA login and enrollment, brute-force lockout, read-only, confinement, revocation, zip download, conflict,
      trusted proxies
- [x] Front-end unit tests (vitest): paths/routes, sizes, types, sorting, crypto envelopes and frame streams,
      upload ids and retry policy (`make test-frontend`)
- [x] Interface validated end to end in the browser (login, listing, editing, creation, i18n, no console errors)

### Build / distribution
- [x] CI (GitHub Actions): gofmt, vet, `go test -race` with coverage, front-end tests, build, smoke test,
      gosec and secret scanning; release workflow builds every platform with the version and publishes the
      binaries plus `SHA256SUMS`; Dependabot for Go, npm and Actions
- [x] Per-build obfuscation key: `make` generates `build/.pepper`, gives it to `assetpack` and to the binary
      (`-ldflags -X …BuildPepper`), so each build seals the front-end under a different key
- [x] Optional obfuscation of the JavaScript shipped to the browser: `make OBFUSCATE_JS=1` (javascript-obfuscator,
      no `eval` / self-defending, compatible with the CSP)

## 3. Roadmap

### Users and security
- [ ] Per-user home directory (restrict each user to a subfolder)
- [ ] Hide account existence for users without MFA too (e.g. always ask for the password, then MFA)

### Not verified yet
- [ ] A real Let's Encrypt issuance with `create-tls --acme` (needs a public domain and port 80)
- [ ] The GitHub Actions workflows on GitHub (validated as YAML only; the repo has no remote yet)
- [ ] Clean-up of abandoned `.fmweb-upload-*.part` files (today they stay until the same file is uploaded again)

# Web File Manager (`fmweb`)

A stand-alone, **encrypted** web file manager in a single executable. The web
interface (HTML/JS/CSS) is embedded in the binary in obfuscated form and served
straight from memory: download one file (`fmweb.exe`, for example) and run it.

```bash
fmweb web -r /data -b 0.0.0.0 -p 8443
```

- **Single binary**: front-end embedded (zip + AES-256-GCM), never extracted to disk.
- **Encrypted `/api` channel**: ECDH P-256 → HKDF-SHA256 → AES-256-GCM, per-session keys kept only in memory.
- **HTTPS by default**, with a self-signed certificate generated on the first run.
- **Pure Go server** (`net/http`).

> The original Python implementation lives in the `python` branch.

## Subcommands

| Command | What it does |
| --- | --- |
| `fmweb create-tls` | Creates a self-signed certificate (`--host`, `--days`, `--force`) or imports one (`--cert`, `--key`) |
| `fmweb add-user`   | Adds or updates a user (`-u`, `-p` / prompt / `--generate`, `--readonly`, `--update`) |
| `fmweb web`        | Starts the server (`-r` root, `-b` bind, `-p` port, `--readonly`, `--show-hidden`, `--exclude`, `--allow-ext`, `--no-tls`, `-y`, `--no-auth`) |
| `fmweb version`    | Version and build information |

Global flags: `--data-dir` (default `./fmweb`), `-v`/`-vv` verbosity, `-D` (= `-vv`), `-q` quiet.

| Level | Flag | What is shown |
| --- | --- | --- |
| quiet | `-q` | errors only |
| info | *(default)* | page access, login/logout, file operations |
| debug | `-v` | plus TLS/HTTP server errors (e.g. `tls: bad certificate` from a browser that does not trust the self-signed certificate yet) and read calls (`list`, `search`, `info`) |
| trace | `-vv` / `-D` | plus every HTTP request (method, path, status, bytes, ms) and the source location of each line |

### `--no-tls`: no payload encryption

Without TLS the browser only exposes WebCrypto on `localhost`, so `--no-tls`
also **disables the payload encryption** of `/api`. That encryption is what
keeps proxies, IDS/IPS and EDR/DLP agents from identifying the traffic: with it
off, logins, passwords, file names and contents travel in clear text.

The console shows this warning and asks `Do you want to continue? [y/N]`
(the default is no). Without a terminal the server refuses to start unless
`-y/--yes` is passed. The UI shows a red banner on the login screen and in the
footer. A server running with TLS refuses unencrypted clients, so a client
cannot downgrade the session to clear text.

### Console log

| Event | Fields |
| --- | --- |
| `access` (GET of `/` and SPA routes) | `ip`, `path`, `ua` |
| `login` / `login failed` / `login blocked` / `logout` | `user`, `ip`, `ua` (login) |
| `upload`, `download`, `open`, `edit`, `create`, `mkdir`, `rename`, `delete`, `copy`, `move`, `chmod`, `zip`, `tar`, `extract` | `user`, `ip`, `path(s)`, plus `size` / `dest` / `mode` depending on the action |

Failures are logged as `WARN … failed` with `err`. Listing and search only show up with `-v`.

### User data (`./fmweb`)

```
fmweb/
├── tls/cert.pem    # certificate
├── tls/key.pem     # private key (0600)
└── users.json      # users (scrypt hashes, 0600)
```

On first run with no users, `web` creates `admin` with a random password and
prints it **only once**. The scrypt format is the same as the Python version's.

> **Note:** on Linux/macOS a binary named `fmweb` and the `./fmweb` directory
> cannot coexist in the same folder. Keep the binary under the name it was
> built with (`fmweb_linux_amd64`, …), put it somewhere else, or use `--data-dir`.

## Build

Requirements: Go 1.24+ and Docker (the front-end is built in a container; the
host does not need Node). With Node installed, use `FRONTEND_BUILDER=local`.

```bash
make            # front-end → obfuscated bundle → binary for the local platform
make all        # cross-compile: windows/linux/darwin × amd64/386/arm64
make test       # Go tests (requires `make assets` once)
make clean
```

Pipeline:

1. `make frontend`: `docker build` of `frontend/` (Vite + React + Tailwind) → `frontend/dist/`
2. `make assets`: `tools/assetpack` seals `dist/` into `pkg/assets/bundle.bin`
3. `go build`: `//go:embed bundle.bin`; at runtime the bundle is decrypted into RAM

Binaries go to `./build/fmweb_<os>_<arch>[.exe]`. `VER=1.2.3 make all` sets the version.

### Front-end development

```bash
fmweb web --no-tls -y -p 8181       # API on http://127.0.0.1:8181 (no encryption)
cd frontend && npm install && npm run dev   # Vite with a proxy to /api
```

## Layout

```
main.go
cmd/                 # cobra: root, web, create-tls, add-user, version
internal/            # ascii (logo), version
pkg/
  api/               # dispatcher of the (decrypted) actions
  assets/            # embed + in-memory serving of the front-end
  assets/bundle/     # obfuscated bundle format (pack/open)
  auth/              # scrypt + users.json
  crypto/            # ECDH/HKDF/AES-GCM, envelopes and download frames
  datadir/           # ./fmweb
  fileops/           # root-confined filesystem operations
  server/            # net/http, /api routes, login rate limit
  session/           # in-memory sessions
  tlsutil/           # certificate generation and import
tools/assetpack/     # build step: dist/ → bundle.bin
frontend/            # React 19 + Tailwind (pure JS), Vite, Dockerfile
```

Requirements, what is implemented and the roadmap are in [TODO.md](TODO.md).

## License

GPL-3.0 © Helvio Junior (M4v3r1ck)

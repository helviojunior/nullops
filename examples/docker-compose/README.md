# nullops with Docker Compose

Run [nullops](https://github.com/helviojunior/nullops) in a container. The image
downloads the latest release from GitHub at build time, and the app is
configured entirely through `NULLOPS_*` environment variables (`nullops web
--env`).

## Quick start

```bash
cp .env.example .env     # edit if you like
docker compose up -d --build
docker compose logs -f   # grab the generated admin password (see below)
```

Then open **https://localhost:8181** (self-signed certificate → accept the
one-time browser warning). With `NULLOPS_NO_TLS=true` the URL is
**http://localhost:8181** instead.

### First-run credentials

On the first start, with an empty data volume, nullops creates an **`admin`**
account with a random password and a self-signed certificate. The password is
printed **once** to the logs — copy it from:

```bash
docker compose logs nullops | grep -A1 'user'
```

It is stored (hashed) in the `/u01` volume, so it keeps working on later starts.
Manage users with `docker compose exec nullops nullops add-user <name>`.

## Configuration

Copy `.env.example` to `.env` and adjust. All values have defaults, so an empty
`.env` works.

| Variable             | Default        | Scope      | Meaning                                                                 |
| -------------------- | -------------- | ---------- | ----------------------------------------------------------------------- |
| `NULLOPS_NO_TLS`     | `false`        | container  | Serve http and drop the browser to the **obfuscated** channel (not encryption). |
| `NULLOPS_YES`        | `true`         | container  | Skip the interactive `--no-tls` confirmation (the container has no TTY). |
| `NULLOPS_BIND`       | `127.0.0.1`    | host       | Interface the port is published on (`0.0.0.0` to expose on the network). |
| `NULLOPS_PORT`       | `8181`         | host       | Host port (the container always listens on `8181`).                     |
| `NULLOPS_DATA_MOUNT` | `nullops-data` | host       | Served files: a named volume or a host path (e.g. `./data`).            |

The **container** variables reach the app through `nullops web --env`; the
**host** variables only shape the compose file. Any other `web` flag can be set
the same way — add `NULLOPS_READONLY=true`, `NULLOPS_ALLOW_EXT=pdf,txt`,
`NULLOPS_MAX_UPLOAD=2G`, etc. to the service `environment` and it is picked up
by `--env` (see the main [README](../../README.md#--env-configure-from-environment-variables)).

## How it works

- **`Dockerfile`** (two stages on `ubuntu:24.04`):
  - *build* — resolves the latest `linux-<arch>` release via the GitHub API
    (same query as [INSTALL.md](../../INSTALL.md)), downloads it with `wget`,
    installs the binary to `/usr/local/sbin`, and symlinks the app's data dir
    `~/.nullops` → `/u01`.
  - *run* — sets the container defaults (`NULLOPS_ROOT=/data`,
    `NULLOPS_BIND=0.0.0.0`, `NULLOPS_PORT=8181`) and runs `nullops web --env`.
  - Multi-arch: BuildKit's `TARGETARCH` selects the matching release asset, so
    it builds on `amd64` and `arm64`.
- **Volumes:**
  - `nullops-home` → `/u01` — app **state**: TLS certificate, sessions, users.
    Persisting this keeps your admin login and certificate across restarts.
  - `/data` — the **files served** by the browser (`NULLOPS_DATA_MOUNT`).

## Common tasks

```bash
# Follow logs
docker compose logs -f nullops

# Stop / start
docker compose down
docker compose up -d

# Serve a host folder instead of a named volume
echo 'NULLOPS_DATA_MOUNT=./data' >> .env && docker compose up -d

# Update to the newest release (the download layer is cached, so force a rebuild)
docker compose build --no-cache && docker compose up -d
```

## Notes

- **TLS vs. obfuscated:** by default the channel is end-to-end encrypted
  (ECDH P-256 + AES-256-GCM). With `NULLOPS_NO_TLS=true` the browser cannot use
  WebCrypto, so payloads are only **obfuscated** (a keystream XOR) — that keeps
  them out of cleartext against passive inspection but is **not** encryption and
  does not stop an active man-in-the-middle. Prefer TLS; use `--no-tls` only
  behind a trusting network or a TLS-terminating proxy.
- **Exposing publicly:** keep `NULLOPS_BIND=127.0.0.1` unless you mean to expose
  the server; set `0.0.0.0` only when you understand the consequences.
- **Reverse proxy:** when a proxy terminates TLS in front of nullops, set
  `NULLOPS_TRUSTED_PROXY=<proxy-ip/cidr>` so client IPs are logged correctly.

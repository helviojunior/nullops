# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Phase 1 — build: fetch the latest nullops release from the GitHub API (the
# same selection used in INSTALL.md), install the runtime tools, and prepare
# the image (symlink the data dir ~/.nullops to the /u01 volume).
# ---------------------------------------------------------------------------
FROM ubuntu:24.04 AS build

# TARGETARCH is provided by BuildKit (amd64, arm64, 386, …); default to amd64
# so a plain `docker build` without buildx still works.
ARG TARGETARCH=amd64

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl jq wget tar \
    && rm -rf /var/lib/apt/lists/*

# Resolve the download URL of the latest release that ships a linux-<arch>
# archive, then download and install the binary (same query as INSTALL.md).
RUN set -eux; \
    url=$(curl -s https://api.github.com/repos/helviojunior/nullops/releases \
        | jq -r --arg filename "linux-${TARGETARCH}.tar.gz\$" \
        '[ .[] | {id: .id, assets: [ .assets[] | select(.name|match($filename)) | {browser_download_url} ]} | select(.assets != []) ] | sort_by(.id) | reverse | first(.[].assets[]) | .browser_download_url'); \
    [ -n "$url" ] && [ "$url" != "null" ] || { echo "no linux-${TARGETARCH} release asset found"; exit 1; }; \
    echo "Downloading $url"; \
    cd /tmp; \
    wget -nv -O nullops-latest.tar.gz "$url"; \
    tar -xzf nullops-latest.tar.gz; \
    install -m 0755 nullops /usr/local/sbin/nullops; \
    rm -f nullops-latest.tar.gz nullops; \
    /usr/local/sbin/nullops version

# Prepare the persistent layout: the app keeps its state (TLS certs, sessions,
# users) in ~/.nullops — point it at the /u01 volume so it survives restarts.
RUN mkdir -p /u01 /data \
    && ln -s /u01 /root/.nullops

# ---------------------------------------------------------------------------
# Phase 2 — run: start the app. Configuration comes from NULLOPS_* variables
# (see `nullops web --env`); the compose file sets them.
# ---------------------------------------------------------------------------
FROM build AS run

# Sensible defaults for a container; override via the environment / compose.
ENV NULLOPS_ROOT=/data \
    NULLOPS_BIND=0.0.0.0 \
    NULLOPS_PORT=8181

WORKDIR /data
EXPOSE 8181
VOLUME ["/u01", "/data"]

CMD ["nullops", "web", "--env"]

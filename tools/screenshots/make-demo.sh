#!/bin/bash
# Creates the demo folder tree shown in the README screenshots.
set -e
D=${1:?usage: make-demo.sh <dir>}
REPO=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$D"/{infra,docs,media,releases,reports}
cat > "$D/infra/deploy.ps1" <<'DEMO_EOF'
<#
.SYNOPSIS
    Deploys the web tier to the staging pool.
.PARAMETER Environment
    Target environment (staging | production).
#>
param(
    [ValidateSet("staging", "production")]
    [string]$Environment = "staging",
    [switch]$WhatIf
)

$ErrorActionPreference = "Stop"
$servers = Get-Content -Path ".\inventory\$Environment.txt"

foreach ($server in $servers) {
    Write-Host "==> Deploying to $server" -ForegroundColor Cyan
    $session = New-PSSession -ComputerName $server
    try {
        Invoke-Command -Session $session -ScriptBlock {
            Stop-WebAppPool -Name "frontend"
            Copy-Item -Path "\\build01\drop\latest\*" -Destination "D:\sites\frontend" -Recurse -Force
            Start-WebAppPool -Name "frontend"
        } -WhatIf:$WhatIf
    }
    finally {
        Remove-PSSession $session
    }
}

Write-Host "Deployment to $Environment finished." -ForegroundColor Green
DEMO_EOF
cat > "$D/infra/backup.sh" <<'DEMO_EOF'
#!/usr/bin/env bash
set -euo pipefail
STAMP=$(date +%Y%m%d_%H%M%S)
tar -czf "/backup/www_${STAMP}.tar.gz" /var/www
find /backup -name 'www_*.tar.gz' -mtime +14 -delete
echo "backup ${STAMP} done"
DEMO_EOF
cat > "$D/infra/docker-compose.yml" <<'DEMO_EOF'
services:
  web:
    image: nginx:1.27-alpine
    ports: ["443:443"]
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
  api:
    image: registry.local/api:2.4.1
    environment:
      LOG_LEVEL: info
DEMO_EOF
cat > "$D/infra/nginx.conf" <<'DEMO_EOF'
events {}
http {
  server {
    listen 443 ssl;
    location / { proxy_pass http://api:8080; }
  }
}
DEMO_EOF
cat > "$D/infra/cleanup.cmd" <<'DEMO_EOF'
cleanup.cmd
@echo off
del /q %TEMP%\*.tmp
DEMO_EOF
cat > "$D/docs/runbook.md" <<'DEMO_EOF'
# Incident runbook

1. Acknowledge the alert.
2. Check the dashboards.
3. Open a PastePad for the timeline.
DEMO_EOF
cat > "$D/docs/README.md" <<'DEMO_EOF'
# Architecture notes

Front-end, API and workers.
DEMO_EOF
cat > "$D/reports/2026-Q3.csv" <<'DEMO_EOF'
quarter,region,revenue
2026-Q3,LATAM,120000
2026-Q3,EMEA,98000
DEMO_EOF
cat > "$D/releases/CHANGELOG.md" <<'DEMO_EOF'
## 1.4.0
- PastePad
- MFA
DEMO_EOF
cp "$REPO/frontend/src/assets/brand/logo-dark.svg" "$D/media/nullops-logo.svg"
# a PDF and a release archive (sizes only matter for the listing)
if command -v cupsfilter >/dev/null; then printf 'NullOps architecture overview\n' > /tmp/arch.txt; cupsfilter /tmp/arch.txt > "$D/docs/architecture.pdf" 2>/dev/null; else printf '%%PDF-1.4\n%%%%EOF\n' > "$D/docs/architecture.pdf"; fi
head -c 200000 /dev/urandom > "$D/releases/nullops_linux_amd64"
(cd "$D/releases" && zip -q nullops_1.4.0_linux_amd64.zip nullops_linux_amd64 && rm nullops_linux_amd64)
echo "demo tree ready in $D"

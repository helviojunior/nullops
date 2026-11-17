# senders

Command-line clients that push a file to a nullops server through a **public
upload link** (`uploadreq_create` → `/public/upload/<token>`), without a
browser. Both speak the same encrypted `/api` channel as the web UI:

1. **Handshake** — `POST /api/handshake` with an ephemeral ECDH P-256 public
   key.
2. **Crypto check / key negotiation** — if the server answers
   `encrypted: true`, derive an AES-256-GCM key
   (ECDH → HKDF-SHA256, salt = session id bytes, info `nullops/aesgcm/v1`).
   `encrypted: false` means the server runs with `--no-tls` and the channel is
   plaintext — the scripts handle both.
3. **Upload** — `public_upload` with `status: true` to find the resume offset,
   then 1 MiB chunks until the last one.

The TLS certificate is **not** verified — these links are commonly served with
a self-signed certificate.

## Python — `upload.py`

Requires the `cryptography` package (`pip install cryptography`).

```bash
./upload.py --url https://host:8443 --token <TOKEN> --file ./report.pdf
# positional form:
./upload.py https://host:8443 <TOKEN> ./report.pdf
```

## PowerShell — `upload.ps1`

Requires PowerShell **7.3+** (.NET 7: HKDF, AesGcm, DeriveRawSecretAgreement).
Windows PowerShell 5.1 runs on .NET Framework, which has none of those, so it
is not supported — install PowerShell 7 and run with `pwsh` (not `powershell`):

```powershell
winget install --id Microsoft.PowerShell       # or: https://aka.ms/powershell-release
pwsh -NoProfile -File ./upload.ps1 -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
```

Run it straight as a script:

```powershell
./upload.ps1 -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
```

Or load the function straight from GitHub (no local copy) and reuse it:

```powershell
iex (irm https://raw.githubusercontent.com/helviojunior/nullops/main/senders/upload.ps1)
Send-NullopsFile -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
```

Or dot-source a local copy and reuse the `Send-NullopsFile` function (each call
opens its own session and returns the stored file info):

```powershell
. ./upload.ps1
Send-NullopsFile -Url https://host:8443 -Token <TOKEN> -File ./a.pdf
Get-ChildItem *.log | ForEach-Object {
    Send-NullopsFile https://host:8443 <TOKEN> $_.FullName -Quiet
}
Get-Help Send-NullopsFile -Full
```

Re-running either script with the same file resumes an interrupted upload.

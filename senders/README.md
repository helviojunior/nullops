# senders

Command-line clients that push a file to a nullops server through a **public
upload link** (`uploadreq_create` → `/public/upload/<token>`), without a
browser. Both speak the same `/api` channel as the web UI:

1. **Handshake** — `POST /api/handshake` with the list of channel modes the
   client accepts (best first) and, when it can encrypt, an ephemeral ECDH
   P-256 public key. The server replies with the mode it picked:
2. **Channel mode**
   - `encrypted` — AES-256-GCM (ECDH → HKDF-SHA256, salt = session id bytes,
     info `nullops/aesgcm/v1`).
   - `obfuscated` — SHA-256 keystream XOR keyed on the session id
     (info `nullops/obf/v1`). **This is not encryption**: it only keeps
     payloads out of cleartext against passive inspection and logs, and gives
     no protection against an active man-in-the-middle. Offered by a server
     started with `--no-tls`.
   - `cleartext` — no payload protection (server `--no-tls --allow-cleartext`).
3. **Upload** — `public_upload` with `status: true` to find the resume offset,
   then 1 MiB chunks until the last one.

The TLS certificate is **not** verified — these links are commonly served with
a self-signed certificate.

## Python — `upload.py`

The `cryptography` package (`pip install cryptography`) enables the `encrypted`
mode. Without it the client still runs, falling back to `obfuscated`/`cleartext`
on a `--no-tls` server.

```bash
./upload.py --url https://host:8443 --token <TOKEN> --file ./report.pdf
# positional form:
./upload.py https://host:8443 <TOKEN> ./report.pdf
```

## PowerShell — `upload.ps1`

Runs on **Windows PowerShell 5.1** and **PowerShell 7+**. The mode is chosen
from the runtime: 7.3+ (.NET 7: HKDF, AesGcm, DeriveRawSecretAgreement) can use
`encrypted`; 5.1 (.NET Framework) has none of those and falls back to
`obfuscated`/`cleartext`. For the strongest channel, install PowerShell 7 and
run with `pwsh`:

```powershell
winget install --id Microsoft.PowerShell       # or: https://aka.ms/powershell-release
pwsh -NoProfile -File ./upload.ps1 -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
```

```powershell
# Windows PowerShell 5.1 also works (obfuscated channel on a --no-tls server):
powershell -File ./upload.ps1 -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
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

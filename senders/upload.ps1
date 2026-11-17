#!/usr/bin/env pwsh
# upload.ps1 — send a file to a nullops server through a public upload link.
#
# Dot-source the file to get a reusable function, then call it as often as
# you like (each call opens its own session):
#
#   . ./upload.ps1
#   Send-NullopsFile -Url https://host:8443 -Token <TOKEN> -File ./a.pdf
#   Send-NullopsFile -Url https://host:8443 -Token <TOKEN> -File ./b.zip
#
# Or run it straight as a script (it just forwards to the function):
#
#   ./upload.ps1 -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
#
# Or load the function straight from GitHub (no local copy) and reuse it:
#
#   iex (irm https://raw.githubusercontent.com/helviojunior/nullops/main/senders/upload.ps1)
#   Send-NullopsFile -Url https://host:8443 -Token <TOKEN> -File ./report.pdf
#
# Channel mode is negotiated from the PowerShell version:
#   - PowerShell 7.3+ (.NET 7) can use the ENCRYPTED mode (HKDF, AesGcm,
#     ECDiffieHellman.DeriveRawSecretAgreement).
#   - Windows PowerShell 5.1 (.NET Framework) lacks those, so it falls back to
#     the OBFUSCATED mode (SHA-256 keystream XOR — not encryption) or cleartext.
# The server advertises which modes it accepts and the client picks the best
# one it can do.

# Script-level parameters: only used when the file is run directly (they are
# forwarded to the function below). Non-mandatory so dot-sourcing never prompts.
param(
    [Parameter(Position = 0)][string]$Url,
    [Parameter(Position = 1)][string]$Token,
    [Parameter(Position = 2)][string]$File,
    [switch]$Quiet
)

function Send-NullopsFile {
    <#
    .SYNOPSIS
        Upload a file to a nullops server through a public upload link.

    .DESCRIPTION
        Speaks the same /api channel as the browser (see pkg/crypto and
        frontend/src/lib/crypto.js). The handshake negotiates a channel mode:

          1. POST /api/handshake with {accept:[...], client_pub?}. The accepted
             modes depend on the runtime: PowerShell 7.3+ offers encrypted,
             obfuscated and cleartext; 5.1 offers obfuscated and cleartext.
          2. The server echoes {mode}:
               encrypted  -> AES-256-GCM (ECDH -> HKDF-SHA256).
               obfuscated -> SHA-256 keystream XOR, keyed on the session id.
                             NOT encryption: keeps payloads out of cleartext
                             against passive inspection only; no MITM protection.
               cleartext  -> no payload protection.
          3. public_upload (status) to find the resume offset, then 1 MiB
             chunks until the last one.

        The TLS certificate is NOT verified, by design: these links are
        commonly served with a self-signed certificate. Re-running with the
        same file resumes an interrupted upload.

        Runs on Windows PowerShell 5.1 (obfuscated/cleartext) and PowerShell
        7.3+ (adds encrypted). Returns a PSCustomObject with the stored file
        name, size, negotiated Mode and whether the channel was Encrypted.

    .PARAMETER Url
        Server base URL, e.g. https://host:8443

    .PARAMETER Token
        Upload token of the public link.

    .PARAMETER File
        Path of the local file to send.

    .PARAMETER Quiet
        Suppress the progress output (the result object is still returned).

    .EXAMPLE
        Send-NullopsFile -Url https://host:8443 -Token 1DETGvSQ -File ./report.pdf

    .EXAMPLE
        . ./upload.ps1
        Get-ChildItem *.log | ForEach-Object {
            Send-NullopsFile https://host:8443 1DETGvSQ $_.FullName -Quiet
        }
    #>
    [CmdletBinding()]
    [OutputType([psobject])]
    param(
        [Parameter(Mandatory, Position = 0)][ValidateNotNullOrEmpty()][string]$Url,
        [Parameter(Mandatory, Position = 1)][ValidateNotNullOrEmpty()][string]$Token,
        [Parameter(Mandatory, Position = 2)][ValidateNotNullOrEmpty()][string]$File,
        [switch]$Quiet
    )

    $ErrorActionPreference = "Stop"
    $HKDFInfo    = [Text.Encoding]::UTF8.GetBytes("nullops/aesgcm/v1")
    $ObfInfo     = [Text.Encoding]::UTF8.GetBytes("nullops/obf/v1")
    $UploadChunk = 1MB

    # Capability detection. Encrypted mode needs .NET 7 primitives (HKDF,
    # AesGcm, DeriveRawSecretAgreement), i.e. PowerShell 7.3+. Modern request
    # switches (-SkipCertificateCheck / -SkipHttpErrorCheck) need PowerShell 6+.
    $psv         = $PSVersionTable.PSVersion
    $CanEncrypt  = ($psv.Major -gt 7) -or ($psv.Major -eq 7 -and $psv.Minor -ge 3)
    $Modern      = $psv.Major -ge 6

    # ---- helpers (nested so dot-sourcing only exposes Send-NullopsFile) ---
    function ConvertTo-B64Url([byte[]]$bytes) {
        [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    }
    function ConvertFrom-B64Url([string]$s) {
        $t = $s.Replace('-', '+').Replace('_', '/')
        switch ($t.Length % 4) { 2 { $t += '==' } 3 { $t += '=' } }
        [Convert]::FromBase64String($t)
    }
    function ConvertFrom-Hex([string]$h) {
        $b = New-Object byte[] ($h.Length / 2)
        for ($i = 0; $i -lt $b.Length; $i++) { $b[$i] = [Convert]::ToByte($h.Substring($i * 2, 2), 16) }
        , $b
    }
    # AES-256-GCM constructor differs across .NET versions.
    function New-AesGcm([byte[]]$key) {
        try { [System.Security.Cryptography.AesGcm]::new($key, 16) }
        catch { [System.Security.Cryptography.AesGcm]::new($key) }
    }
    function Write-Note([string]$msg, [string]$color = "Cyan") {
        if (-not $Quiet) { Write-Host $msg -ForegroundColor $color }
    }

    # Keystream XOR (obfuscated mode), mirroring pkg/crypto/obfuscate.go.
    # Obfuscation, not encryption: it only keeps payloads out of cleartext.
    function Invoke-ObfXor([byte[]]$secret, [byte[]]$nonce, [byte[]]$buf) {
        $n = $buf.Length
        $out = New-Object byte[] $n
        if ($n -eq 0) { return , $out }
        $sha = [System.Security.Cryptography.SHA256]::Create()
        try {
            $block = $null; $off = 32; $ctr = 0
            $pre = New-Object byte[] ($ObfInfo.Length + $secret.Length + $nonce.Length + 4)
            [Array]::Copy($ObfInfo, 0, $pre, 0, $ObfInfo.Length)
            [Array]::Copy($secret, 0, $pre, $ObfInfo.Length, $secret.Length)
            [Array]::Copy($nonce, 0, $pre, $ObfInfo.Length + $secret.Length, $nonce.Length)
            $ctrPos = $ObfInfo.Length + $secret.Length + $nonce.Length
            for ($i = 0; $i -lt $n; $i++) {
                if ($off -eq 32) {
                    $cb = [BitConverter]::GetBytes([uint32]$ctr) # little-endian target
                    [Array]::Copy($cb, 0, $pre, $ctrPos, 4)
                    $block = $sha.ComputeHash($pre)
                    $ctr++; $off = 0
                }
                $out[$i] = $buf[$i] -bxor $block[$off]
                $off++
            }
        } finally { $sha.Dispose() }
        , $out
    }

    function Protect-Obf([byte[]]$secret, [byte[]]$pt) {
        $iv = New-Object byte[] 12
        [System.Security.Cryptography.RandomNumberGenerator]::Fill($iv)
        @{ iv = (ConvertTo-B64Url $iv); data = (ConvertTo-B64Url (Invoke-ObfXor $secret $iv $pt)) }
    }

    function Unprotect-Obf([byte[]]$secret, $envelope) {
        $iv  = ConvertFrom-B64Url $envelope.iv
        $buf = ConvertFrom-B64Url $envelope.data
        [Text.Encoding]::UTF8.GetString((Invoke-ObfXor $secret $iv $buf))
    }

    function Invoke-Api([hashtable]$session, [string]$path, [string]$bodyJson, [hashtable]$headers) {
        $uri = $session.Base + $path
        if ($session.Modern) {
            $r = Invoke-WebRequest -Uri $uri -Method Post -Body $bodyJson `
                -ContentType "application/json" -Headers $headers `
                -SkipCertificateCheck -SkipHttpErrorCheck
            return [PSCustomObject]@{ Status = [int]$r.StatusCode; Body = $r.Content }
        }
        # Windows PowerShell 5.1: no -Skip* switches. The certificate callback is
        # set once in the body; non-2xx throws, so read the body from the error.
        try {
            $r = Invoke-WebRequest -Uri $uri -Method Post -Body $bodyJson `
                -ContentType "application/json" -Headers $headers -UseBasicParsing
            [PSCustomObject]@{ Status = [int]$r.StatusCode; Body = $r.Content }
        } catch [System.Net.WebException] {
            $resp = $_.Exception.Response
            if ($null -eq $resp) { throw }
            $sr = New-Object IO.StreamReader($resp.GetResponseStream())
            try { $txt = $sr.ReadToEnd() } finally { $sr.Close() }
            [PSCustomObject]@{ Status = [int]$resp.StatusCode; Body = $txt }
        }
    }

    function Protect-Payload([byte[]]$key, [byte[]]$pt) {
        $iv = New-Object byte[] 12
        [System.Security.Cryptography.RandomNumberGenerator]::Fill($iv)
        $ct  = New-Object byte[] $pt.Length
        $tag = New-Object byte[] 16
        $aes = New-AesGcm $key
        try { $aes.Encrypt($iv, $pt, $ct, $tag, $null) } finally { $aes.Dispose() }
        $data = New-Object byte[] ($ct.Length + 16)
        [Array]::Copy($ct, 0, $data, 0, $ct.Length)
        [Array]::Copy($tag, 0, $data, $ct.Length, 16)
        @{ iv = (ConvertTo-B64Url $iv); data = (ConvertTo-B64Url $data) }
    }

    function Unprotect-Envelope([byte[]]$key, $envelope) {
        $iv  = ConvertFrom-B64Url $envelope.iv
        $buf = ConvertFrom-B64Url $envelope.data
        $ctLen = $buf.Length - 16
        $ct  = New-Object byte[] $ctLen
        $tag = New-Object byte[] 16
        [Array]::Copy($buf, 0, $ct, 0, $ctLen)
        [Array]::Copy($buf, $ctLen, $tag, 0, 16)
        $pt  = New-Object byte[] $ctLen
        $aes = New-AesGcm $key
        try { $aes.Decrypt($iv, $ct, $tag, $pt, $null) } finally { $aes.Dispose() }
        [Text.Encoding]::UTF8.GetString($pt)
    }

    # handshake: negotiate the channel mode from what this runtime can do
    function Connect-Server([hashtable]$session) {
        $ecdh = $null
        $accept = if ($session.CanEncrypt) { @("encrypted", "obfuscated", "cleartext") }
                  else { @("obfuscated", "cleartext") }
        $bodyObj = @{ accept = $accept }
        if ($session.CanEncrypt) {
            $ecdh = [System.Security.Cryptography.ECDiffieHellman]::Create(
                [System.Security.Cryptography.ECCurve]::CreateFromFriendlyName("nistP256"))
            $pub = $ecdh.PublicKey.ExportParameters()
            $clientPub = New-Object byte[] 65
            $clientPub[0] = 4
            [Array]::Copy($pub.Q.X, 0, $clientPub, 1 + (32 - $pub.Q.X.Length), $pub.Q.X.Length)
            [Array]::Copy($pub.Q.Y, 0, $clientPub, 33 + (32 - $pub.Q.Y.Length), $pub.Q.Y.Length)
            $bodyObj.client_pub = (ConvertTo-B64Url $clientPub)
        }

        $resp = Invoke-Api $session "/api/handshake" ($bodyObj | ConvertTo-Json -Compress) @{ }
        $data = $resp.Body | ConvertFrom-Json
        if ($data.code -eq "secure_context") { throw "server requires a secure context for the handshake" }
        if ($resp.Status -ne 200) { throw "handshake failed: $($data.error)" }

        $session.SessionId = $data.session_id
        # Back-compat with a server that predates mode negotiation.
        $session.Mode = if ($data.mode) { $data.mode }
                        elseif ($data.encrypted -eq $false) { "cleartext" } else { "encrypted" }

        if ($session.Mode -eq "cleartext") {
            $session.Plain = $true
            return "cleartext (no payload protection)"
        }
        if ($session.Mode -eq "obfuscated") {
            $session.Obf = ConvertFrom-Hex $session.SessionId
            return "obfuscated (keystream XOR, not encryption)"
        }

        # encrypted -> negotiate the shared AES key
        if (-not $session.CanEncrypt) { throw "server selected encrypted mode but this PowerShell is < 7.3" }
        $serverPub = ConvertFrom-B64Url $data.server_pub
        $peerParams = [System.Security.Cryptography.ECParameters]::new()
        $peerParams.Curve = [System.Security.Cryptography.ECCurve]::CreateFromFriendlyName("nistP256")
        $q = [System.Security.Cryptography.ECPoint]::new()
        $q.X = [byte[]]($serverPub[1..32])
        $q.Y = [byte[]]($serverPub[33..64])
        $peerParams.Q = $q
        $peer = [System.Security.Cryptography.ECDiffieHellman]::Create($peerParams)
        $shared = $ecdh.DeriveRawSecretAgreement($peer.PublicKey)

        $salt = ConvertFrom-Hex $session.SessionId
        $session.Key = [System.Security.Cryptography.HKDF]::DeriveKey(
            [System.Security.Cryptography.HashAlgorithmName]::SHA256, $shared, 32, $salt, $HKDFInfo)
        return "encrypted (AES-256-GCM negotiated)"
    }

    function Invoke-Call([hashtable]$session, [string]$action, [hashtable]$payload) {
        $json = $payload | ConvertTo-Json -Compress -Depth 10
        if ($session.Plain) {
            $body = $json
        } elseif ($null -ne $session.Obf) {
            $body = (Protect-Obf $session.Obf ([Text.Encoding]::UTF8.GetBytes($json))) | ConvertTo-Json -Compress
        } else {
            $body = (Protect-Payload $session.Key ([Text.Encoding]::UTF8.GetBytes($json))) | ConvertTo-Json -Compress
        }
        $resp = Invoke-Api $session "/api/$action" $body @{ "X-Session-Id" = $session.SessionId }
        $data = $resp.Body | ConvertFrom-Json
        if ($resp.Status -ne 200 -or (-not $session.Plain -and -not $data.iv)) {
            throw "request failed: $($data.error)"
        }
        $out = if ($session.Plain) { $data }
               elseif ($null -ne $session.Obf) { (Unprotect-Obf $session.Obf $data) | ConvertFrom-Json }
               else { (Unprotect-Envelope $session.Key $data) | ConvertFrom-Json }
        if ($out.ok -eq $false) { throw "error: $($out.error)" }
        if ($null -ne $out.result) { $out.result } else { $out }
    }

    function Invoke-CallRetry([hashtable]$session, [string]$action, [hashtable]$payload, [int]$attempts = 4) {
        for ($i = 0; $i -lt $attempts; $i++) {
            try { return Invoke-Call $session $action $payload }
            catch {
                if ($i -eq $attempts - 1) { throw }
                Start-Sleep -Seconds ([Math]::Pow(2, $i))
            }
        }
    }

    # ---- body ------------------------------------------------------------
    if (-not (Test-Path -LiteralPath $File -PathType Leaf)) { throw "not a file: $File" }
    $item  = Get-Item -LiteralPath $File
    $name  = $item.Name
    $size  = [long]$item.Length
    $mtime = $item.LastWriteTimeUtc.Ticks

    $sha  = [System.Security.Cryptography.SHA256]::Create()
    $hash = $sha.ComputeHash([Text.Encoding]::UTF8.GetBytes("$name|$size|$mtime"))
    $upid = ($hash | ForEach-Object { $_.ToString("x2") }) -join ""

    $session = @{ Base = $Url.TrimEnd('/'); SessionId = $null; Key = $null; Obf = $null
        Plain = $false; Mode = $null; Modern = $Modern; CanEncrypt = $CanEncrypt }
    # Windows PowerShell 5.1 cannot skip certificate validation per-request, so
    # accept any certificate process-wide (these links use self-signed certs).
    if (-not $Modern) {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        [Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
    }
    $fields  = @{ token = $Token; name = $name; upload_id = $upid; size = $size }

    Write-Note "[*] connecting to $($session.Base)"
    Write-Note "[*] $(Connect-Server $session)"

    $status = Invoke-Call $session "public_upload" ($fields + @{ status = $true })
    $offset = [long][Math]::Min([long]($status.received), $size)
    if ($offset -gt 0) { Write-Note "[*] resuming at $offset/$size bytes" }

    $fs  = [IO.File]::OpenRead($File)
    $buf = New-Object byte[] $UploadChunk
    $res = $null
    try {
        while ($true) {
            $fs.Position = $offset
            $read = $fs.Read($buf, 0, $UploadChunk)
            if ($read -lt 0) { $read = 0 }
            $chunk = New-Object byte[] $read
            [Array]::Copy($buf, 0, $chunk, 0, $read)
            $end  = $offset + $read
            $last = $end -ge $size
            $res  = Invoke-CallRetry $session "public_upload" ($fields + @{
                    offset = $offset; last = $last; chunk = (ConvertTo-B64Url $chunk) })
            $offset = $end
            $pct = if ($size) { 100.0 * $offset / $size } else { 100.0 }
            if (-not $Quiet) { Write-Host ("`r[*] {0}/{1} bytes ({2:N1}%)" -f $offset, $size, $pct) -NoNewline }
            if ($last) {
                if (-not $Quiet) { Write-Host "" }
                Write-Note "[+] stored as $($res.name) ($($res.size) bytes)" "Green"
                break
            }
        }
    } finally {
        $fs.Close()
    }

    [PSCustomObject]@{
        Name      = $res.name
        Size      = [long]$res.size
        Mode      = $session.Mode
        Encrypted = ($session.Mode -eq "encrypted")
    }
}

# Run only when arguments were actually passed (e.g. "./upload.ps1 -Url ...").
# Loading the file to get the function — dot-sourcing (". ./upload.ps1") or
# "iex (irm <url>)" — passes none, so it just defines Send-NullopsFile.
if ($PSBoundParameters.Count -gt 0) {
    Send-NullopsFile @PSBoundParameters
}

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
# Requires PowerShell 7.3+ (.NET 7): uses HKDF, AesGcm,
# ECDiffieHellman.DeriveRawSecretAgreement and -SkipCertificateCheck /
# -SkipHttpErrorCheck.

#requires -Version 7.3

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
        Speaks the same encrypted /api channel as the browser (see pkg/crypto
        and frontend/src/lib/crypto.js):

          1. POST /api/handshake with an ephemeral ECDH P-256 public key.
          2. If the server answers encrypted:true it negotiates an AES-256-GCM
             key (ECDH -> HKDF-SHA256, salt = session id bytes). encrypted:false
             means the server runs without TLS payload encryption and the
             channel is plain.
          3. public_upload (status) to find the resume offset, then 1 MiB
             chunks until the last one.

        The TLS certificate is NOT verified, by design: these links are
        commonly served with a self-signed certificate. Re-running with the
        same file resumes an interrupted upload.

        Returns a PSCustomObject with the stored file name, size and whether
        the channel was encrypted.

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
    $UploadChunk = 1MB

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

    function Invoke-Api([hashtable]$session, [string]$path, [string]$bodyJson, [hashtable]$headers) {
        $r = Invoke-WebRequest -Uri ($session.Base + $path) -Method Post -Body $bodyJson `
            -ContentType "application/json" -Headers $headers `
            -SkipCertificateCheck -SkipHttpErrorCheck
        [PSCustomObject]@{ Status = [int]$r.StatusCode; Body = $r.Content }
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

    # handshake: check crypto, negotiate the key if enabled
    function Connect-Server([hashtable]$session) {
        $ecdh = [System.Security.Cryptography.ECDiffieHellman]::Create(
            [System.Security.Cryptography.ECCurve]::CreateFromFriendlyName("nistP256"))
        $pub = $ecdh.PublicKey.ExportParameters()
        $clientPub = New-Object byte[] 65
        $clientPub[0] = 4
        [Array]::Copy($pub.Q.X, 0, $clientPub, 1 + (32 - $pub.Q.X.Length), $pub.Q.X.Length)
        [Array]::Copy($pub.Q.Y, 0, $clientPub, 33 + (32 - $pub.Q.Y.Length), $pub.Q.Y.Length)

        $body = @{ client_pub = (ConvertTo-B64Url $clientPub) } | ConvertTo-Json -Compress
        $resp = Invoke-Api $session "/api/handshake" $body @{ }
        $data = $resp.Body | ConvertFrom-Json
        if ($data.code -eq "secure_context") { throw "server requires a secure context for the handshake" }
        if ($resp.Status -ne 200) { throw "handshake failed: $($data.error)" }

        $session.SessionId = $data.session_id
        if ($data.encrypted -eq $false) {
            $session.Plain = $true
            return "plaintext (server has no payload encryption)"
        }

        # crypto is enabled -> negotiate the shared AES key
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
        } else {
            $body = (Protect-Payload $session.Key ([Text.Encoding]::UTF8.GetBytes($json))) | ConvertTo-Json -Compress
        }
        $resp = Invoke-Api $session "/api/$action" $body @{ "X-Session-Id" = $session.SessionId }
        $data = $resp.Body | ConvertFrom-Json
        if ($resp.Status -ne 200 -or (-not $session.Plain -and -not $data.iv)) {
            throw "request failed: $($data.error)"
        }
        $out = if ($session.Plain) { $data } else { (Unprotect-Envelope $session.Key $data) | ConvertFrom-Json }
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

    $session = @{ Base = $Url.TrimEnd('/'); SessionId = $null; Key = $null; Plain = $false }
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
        Encrypted = -not $session.Plain
    }
}

# Run only when arguments were actually passed (e.g. "./upload.ps1 -Url ...").
# Loading the file to get the function — dot-sourcing (". ./upload.ps1") or
# "iex (irm <url>)" — passes none, so it just defines Send-NullopsFile.
if ($PSBoundParameters.Count -gt 0) {
    Send-NullopsFile @PSBoundParameters
}

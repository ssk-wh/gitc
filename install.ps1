# gitc installer (Windows)
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ssk-wh/gitc/master/install.ps1 | iex"
$ErrorActionPreference = 'Stop'
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
}

$Repo = if ($env:GITC_REPO) { $env:GITC_REPO } else { 'ssk-wh/gitc' }
$BinDir = if ($env:GITC_INSTALL_DIR) { $env:GITC_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'gitc\bin' }
$Version = if ($env:GITC_VERSION) { $env:GITC_VERSION } else { $null }
$Mirror = if ($env:GITC_MIRROR) { $env:GITC_MIRROR } else { 'https://gh-proxy.com/' }

function Download-Url([string]$url, [string]$dest) {
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $dest
    } catch {
        Write-Host 'Direct download failed, trying mirror...'
        Invoke-WebRequest -UseBasicParsing -Uri ($Mirror + $url) -OutFile $dest
    }
}

# Follow the 302 from releases/latest to get the tag (API may be rate limited).
function Get-LatestTag {
    $url = "https://github.com/$Repo/releases/latest"
    try {
        $final = & curl.exe -fsSL -o NUL -w "%{url_effective}" $url 2>$null
        if ($LASTEXITCODE -eq 0 -and $final -match '/tag/([^/?#]+)') {
            return $Matches[1]
        }
    } catch {
    }
    $loc = $null
    try {
        $req = [Net.HttpWebRequest]::Create($url)
        $req.Method = 'HEAD'
        $req.AllowAutoRedirect = $false
        $req.UserAgent = 'gitc-installer'
        $resp = $req.GetResponse()
        $loc = $resp.Headers['Location']
        $resp.Close()
    } catch [Net.WebException] {
        if ($_.Exception.Response) {
            $loc = $_.Exception.Response.Headers['Location']
        }
    } catch {
    }
    if ($loc -match '/tag/([^/?#]+)') {
        return $Matches[1]
    }
    try {
        $rel = Invoke-RestMethod -Headers @{ 'User-Agent' = 'gitc-installer' } -Uri "https://api.github.com/repos/$Repo/releases/latest"
        if ($rel.tag_name) { return $rel.tag_name }
    } catch {
    }
    return $null
}

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

function Install-Binary([string]$src) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $dest = Join-Path $BinDir 'gitc.exe'
    Copy-Item -Force $src $dest
    # Only the default directory updates the user PATH.
    if (-not $env:GITC_INSTALL_DIR) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not $userPath) { $userPath = '' }
        if ($userPath -notlike "*$BinDir*") {
            [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $BinDir), 'User')
            Write-Host "Added $BinDir to user PATH (open a new terminal to apply)"
        }
    }
    $env:Path = "$BinDir;$env:Path"
    Write-Host "Installed: $dest"
    & $dest --version
}

$tmp = Join-Path $env:TEMP ('gitc-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    $tag = $Version
    if (-not $tag) {
        Write-Host 'Querying latest release...'
        $tag = Get-LatestTag
    }
    if (-not $tag) {
        throw 'Cannot resolve the latest release tag; set GITC_VERSION and retry.'
    }

    $name = "gitc-windows-$Arch.exe"
    $base = "https://github.com/$Repo/releases/download/$tag"
    $file = Join-Path $tmp 'gitc.exe'
    Write-Host "Downloading $name ($tag)..."
    try {
        Download-Url "$base/$name" $file
    } catch {
        throw "Release download failed: $($_.Exception.Message). Set GITC_VERSION or GITC_MIRROR and retry."
    }

    try {
        $sumFile = Join-Path $tmp 'SHA256SUMS'
        Download-Url "$base/SHA256SUMS" $sumFile
        $line = Get-Content $sumFile | Where-Object { $_ -match [regex]::Escape($name) } | Select-Object -First 1
        if ($line) {
            $expected = ($line -split '\s+')[0].ToLower()
            $actual = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
            if ($actual -ne $expected) { throw 'SHA256 mismatch' }
            Write-Host 'SHA256 verified'
        }
    } catch {
        Write-Host "SHA256 check skipped: $($_.Exception.Message)"
    }

    Install-Binary $file
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

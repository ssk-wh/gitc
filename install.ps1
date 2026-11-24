# gitc 一键安装（Windows）
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ssk-wh/gitc/master/install.ps1 | iex"
$ErrorActionPreference = 'Stop'

$Repo = if ($env:GITC_REPO) { $env:GITC_REPO } else { 'ssk-wh/gitc' }
$BinDir = if ($env:GITC_INSTALL_DIR) { $env:GITC_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'gitc\bin' }
$Version = if ($env:GITC_VERSION) { $env:GITC_VERSION } else { $null }
function Download-Url([string]$url, [string]$dest) {
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $dest
    } catch {
        $mirror = if ($env:GITC_MIRROR) { $env:GITC_MIRROR } else { 'https://gh-proxy.com/' }
        Invoke-WebRequest -UseBasicParsing -Uri ($mirror + $url) -OutFile $dest
    }
}
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "不支持的架构: $env:PROCESSOR_ARCHITECTURE" }
}

function Install-Binary([string]$src) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $dest = Join-Path $BinDir 'gitc.exe'
    Copy-Item -Force $src $dest
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($AddToPath -and $userPath -notlike "*$BinDir*") {
        [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $BinDir), 'User')
        Write-Host "已将 $BinDir 加入用户 PATH（重新打开终端后生效）"
    }
    $env:Path = "$BinDir;$env:Path"
    Write-Host "已安装: $dest"
    & $dest --version
}

$tmp = Join-Path $env:TEMP ('gitc-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    $rel = $null
    if ($Version) {
        $rel = [pscustomobject]@{ tag_name = $Version; assets = @(
            [pscustomobject]@{ name = "gitc-windows-$Arch.exe"; browser_download_url = "https://github.com/$Repo/releases/download/$Version/gitc-windows-$Arch.exe" },
            [pscustomobject]@{ name = 'SHA256SUMS'; browser_download_url = "https://github.com/$Repo/releases/download/$Version/SHA256SUMS" }
        ) }
    } else {
        Write-Host '查询最新 release…'
        try {
            $rel = Invoke-RestMethod -Headers @{ 'User-Agent' = 'gitc-installer' } -Uri "https://api.github.com/repos/$Repo/releases/latest"
        } catch {
            $rel = $null
        }
    }

    if ($rel -and $rel.tag_name) {
        $asset = $rel.assets | Where-Object { $_.name -eq "gitc-windows-$Arch.exe" } | Select-Object -First 1
        if ($asset) {
            $file = Join-Path $tmp 'gitc.exe'
            Write-Host "下载 $($asset.name) ($($rel.tag_name))…"
            Download-Url $asset.browser_download_url $file
            $sumAsset = $rel.assets | Where-Object { $_.name -eq 'SHA256SUMS' } | Select-Object -First 1
            if ($sumAsset) {
                $sumFile = Join-Path $tmp 'SHA256SUMS'
                Download-Url $sumAsset.browser_download_url $sumFile
                $line = Get-Content $sumFile | Where-Object { $_ -match [regex]::Escape("gitc-windows-$Arch.exe") } | Select-Object -First 1
                if ($line) {
                    $expected = ($line -split '\s+')[0].ToLower()
                    $actual = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
                    if ($actual -ne $expected) { throw 'SHA256 校验失败' }
                }
            }
            Install-Binary $file
            exit 0
        }
    }

    # 回退：源码构建
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw '没有可用的 release，且未安装 git' }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw '没有可用的 release，且未安装 Go' }
    Write-Host '从源码构建…'
    $src = Join-Path $tmp 'src'
    git clone --depth 1 "https://github.com/$Repo" $src | Out-Null
    Push-Location $src
    try {
        $env:CGO_ENABLED = '0'
        $out = Join-Path $tmp 'gitc.exe'
        go build -trimpath -ldflags '-s -w' -o $out .
    } finally {
        Pop-Location
    }
    Install-Binary $out
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

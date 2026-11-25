# gitc 一键安装（Windows）
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ssk-wh/gitc/master/install.ps1 | iex"
$ErrorActionPreference = 'Stop'
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # 老系统不支持 Tls12 时保持默认
}

$Repo = if ($env:GITC_REPO) { $env:GITC_REPO } else { 'ssk-wh/gitc' }
$BinDir = if ($env:GITC_INSTALL_DIR) { $env:GITC_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'gitc\bin' }
$Version = if ($env:GITC_VERSION) { $env:GITC_VERSION } else { $null }
$Mirror = if ($env:GITC_MIRROR) { $env:GITC_MIRROR } else { 'https://gh-proxy.com/' }

function Download-Url([string]$url, [string]$dest) {
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $dest
    } catch {
        Write-Host '直连失败，尝试镜像下载…'
        Invoke-WebRequest -UseBasicParsing -Uri ($Mirror + $url) -OutFile $dest
    }
}

# 跟随 github.com/<repo>/releases/latest 的 302 拿到 tag，避免 API 被限流/拦截。
function Get-LatestTag {
    $url = "https://github.com/$Repo/releases/latest"
    # Windows 10/11 自带 curl.exe：直接跟随 302 读取最终 URL，最稳且不受 API 限流影响
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
    # 兜底：GitHub API（可能被限流）
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
    default { throw "不支持的架构: $env:PROCESSOR_ARCHITECTURE" }
}

function Install-Binary([string]$src) {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $dest = Join-Path $BinDir 'gitc.exe'
    Copy-Item -Force $src $dest
    # 自定义安装目录不修改用户 PATH；默认目录自动配置
    if (-not $env:GITC_INSTALL_DIR) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not $userPath) { $userPath = '' }
        if ($userPath -notlike "*$BinDir*") {
            [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $BinDir), 'User')
            Write-Host "已将 $BinDir 加入用户 PATH（重新打开终端后生效）"
        }
    }
    $env:Path = "$BinDir;$env:Path"
    Write-Host "已安装: $dest"
    & $dest --version
}

$tmp = Join-Path $env:TEMP ('gitc-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    $tag = $Version
    if (-not $tag) {
        Write-Host '查询最新 release…'
        $tag = Get-LatestTag
    }

    if ($tag) {
        $name = "gitc-windows-$Arch.exe"
        $base = "https://github.com/$Repo/releases/download/$tag"
        $file = Join-Path $tmp 'gitc.exe'
        Write-Host "下载 $name ($tag)…"
        $ok = $true
        try {
            Download-Url "$base/$name" $file
        } catch {
            $ok = $false
            Write-Host "Release 下载失败: $($_.Exception.Message)"
        }
        if ($ok) {
            try {
                $sumFile = Join-Path $tmp 'SHA256SUMS'
                Download-Url "$base/SHA256SUMS" $sumFile
                $line = Get-Content $sumFile | Where-Object { $_ -match [regex]::Escape($name) } | Select-Object -First 1
                if ($line) {
                    $expected = ($line -split '\s+')[0].ToLower()
                    $actual = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
                    if ($actual -ne $expected) { throw 'SHA256 校验失败' }
                    Write-Host 'SHA256 校验通过'
                }
            } catch {
                Write-Host "SHA256 校验跳过: $($_.Exception.Message)"
            }
            Install-Binary $file
            exit 0
        }
    } else {
        Write-Host '未获取到最新版本号'
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

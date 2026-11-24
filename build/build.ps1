# 构建 gitc（Windows）
$ErrorActionPreference = 'Stop'
# 未在 PATH 中时回退查找常见 Go 安装位置
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    foreach ($c in @("$env:ProgramFiles\Go\bin\go.exe", "D:\tools\go\bin\go.exe")) {
        if (Test-Path $c) { $env:PATH = (Split-Path $c) + ';' + $env:PATH; break }
    }
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw '未找到 go，请安装 Go 1.22+ 或将其加入 PATH'
}
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path $out | Out-Null
$env:CGO_ENABLED = '0'
Push-Location $root
try {
    go build -trimpath -ldflags '-s -w' -o (Join-Path $out 'gitc.exe') .
    $size = (Get-Item (Join-Path $out 'gitc.exe')).Length / 1MB
    Write-Host ("构建完成: {0} ({1:N1} MB)" -f (Join-Path $out 'gitc.exe'), $size)
} finally {
    Pop-Location
}

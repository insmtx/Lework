$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"

$root = Get-LeworkRepoRoot
Set-OptionalGoBuildEnvironment
$goExe = Get-GoExe

if (-not (Test-Path "$root\bundles")) {
    New-Item -ItemType Directory -Path "$root\bundles" | Out-Null
}

Set-Location $root
Write-Host '[Lework] Rebuilding backend...' -ForegroundColor Cyan
& $goExe build -v -o "$root\bundles\lework.exe" .\backend\cmd\lework\

if ($LASTEXITCODE -ne 0) {
    throw 'Backend rebuild failed.'
}

Write-Host '[Lework] Backend rebuild completed.' -ForegroundColor Green

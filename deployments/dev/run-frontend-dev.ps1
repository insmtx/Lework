$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"

$root = Get-LerosRepoRoot
$pnpmExe = Get-PnpmExe
$runtimeState = Get-ConfiguredDevRuntimeState
$frontendWebRoot = Join-Path $root 'frontend\apps\web'
$envFilePath = Join-Path $frontendWebRoot '.env.local'

$env:NEXT_PUBLIC_LEROS_API_BASE_URL = "$($runtimeState.apiBaseUrl)"
Set-Content -Path $envFilePath -Value "NEXT_PUBLIC_LEROS_API_BASE_URL=$($runtimeState.apiBaseUrl)" -Encoding UTF8

Set-Location "$root\frontend"
Write-Host "[Leros][Frontend] Starting on http://localhost:3005 (API: $($runtimeState.apiBaseUrl))" -ForegroundColor Cyan
& $pnpmExe run dev:web

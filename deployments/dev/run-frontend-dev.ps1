$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"

$root = Get-LerosRepoRoot
$pnpmExe = Get-PnpmExe
$runtimeState = Get-ConfiguredDevRuntimeState
$frontendWebRoot = Join-Path $root 'frontend\apps\web'
$envFilePath = Join-Path $frontendWebRoot '.env.local'

$webApiBaseUrl = '/v1'
$env:NEXT_PUBLIC_LEROS_API_BASE_URL = $webApiBaseUrl
# 中文注释：Web 本地开发走 Next 同源代理，避免浏览器直接跨端口请求本地后端时出现跨域问题。
Set-Content -Path $envFilePath -Value "NEXT_PUBLIC_LEROS_API_BASE_URL=$webApiBaseUrl" -Encoding UTF8

Set-Location "$root\frontend"
Write-Host "[Leros][Frontend] Starting on http://localhost:3005 (API Proxy: $webApiBaseUrl -> $($runtimeState.apiBaseUrl))" -ForegroundColor Cyan
& $pnpmExe run dev:web

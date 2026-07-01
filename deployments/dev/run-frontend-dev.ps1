$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"

$root = Get-LerosRepoRoot
$pnpmExe = Get-PnpmExe
$runtimeState = Get-ConfiguredDevRuntimeState
$frontendWebRoot = Join-Path $root 'frontend\apps\web'
$envFilePath = Join-Path $frontendWebRoot '.env.local'

# 仅在 .env.local 不存在时自动生成，避免覆盖开发者本地 API 配置
if (Test-Path -LiteralPath $envFilePath) {
	Write-Host "[Leros][Frontend] Starting on http://localhost:3005 (using existing .env.local)" -ForegroundColor Cyan
} else {
	$env:NEXT_PUBLIC_LEROS_API_BASE_URL = "$($runtimeState.apiBaseUrl)"
	Set-Content -Path $envFilePath -Value "NEXT_PUBLIC_LEROS_API_BASE_URL=$($runtimeState.apiBaseUrl)" -Encoding UTF8
	Write-Host "[Leros][Frontend] Starting on http://localhost:3005 (API: $($runtimeState.apiBaseUrl))" -ForegroundColor Cyan
}

Set-Location "$root\frontend"
& $pnpmExe run dev:web

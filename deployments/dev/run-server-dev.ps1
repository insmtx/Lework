$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LerosRepoRoot
$runtimeState = Get-ConfiguredDevRuntimeState
$env:LEROS_STORAGE_LOCAL_DIR = "$root\leros-storage"
$workspaceMountRoot = "$root\.leros-workspace"

if (-not (Test-Path $env:LEROS_STORAGE_LOCAL_DIR)) {
    New-Item -ItemType Directory -Path $env:LEROS_STORAGE_LOCAL_DIR | Out-Null
}

$resolvedServerConfig = New-ResolvedServerConfig -RepoRoot $root -ServerPort $runtimeState.serverPort

Set-Location $root
Write-Host "[Leros][Server] Starting on http://localhost:$($runtimeState.serverPort)" -ForegroundColor Cyan
& "$root\bundles\leros.exe" server --config $resolvedServerConfig --workspace-root $workspaceMountRoot

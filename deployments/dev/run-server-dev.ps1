$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LeworkRepoRoot
$runtimeState = Get-ConfiguredDevRuntimeState
$env:LEWORK_STORAGE_LOCAL_DIR = "$root\lework-storage"
$workspaceMountRoot = "$root\.lework-workspace"

Ensure-LatestBackendBinary -RepoRoot $root

if (-not (Test-Path $env:LEWORK_STORAGE_LOCAL_DIR)) {
    New-Item -ItemType Directory -Path $env:LEWORK_STORAGE_LOCAL_DIR | Out-Null
}

$resolvedServerConfig = New-ResolvedServerConfig -RepoRoot $root -ServerPort $runtimeState.serverPort

Set-Location $root
Write-Host "[Lework][Server] Starting on http://localhost:$($runtimeState.serverPort)" -ForegroundColor Cyan
& "$root\bundles\lework.exe" server --config $resolvedServerConfig --workspace-root $workspaceMountRoot

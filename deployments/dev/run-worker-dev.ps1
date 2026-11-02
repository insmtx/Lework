$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LeworkRepoRoot
$runtimeState = Get-ConfiguredDevRuntimeState
$env:LEWORK_DEV = 'true'
$workspaceMountRoot = "$root\.lework-workspace"

Ensure-LatestBackendBinary -RepoRoot $root

$resolvedWorkerConfig = New-ResolvedWorkerConfig -RepoRoot $root -ServerPort $runtimeState.serverPort

Set-Location $root
Write-Host "[Lework][Worker] Starting on http://localhost:$($runtimeState.workerPort)" -ForegroundColor Cyan
& "$root\bundles\lework.exe" worker --worker-id 1 --config $resolvedWorkerConfig --listen-addr ":$($runtimeState.workerPort)" --workspace-root $workspaceMountRoot

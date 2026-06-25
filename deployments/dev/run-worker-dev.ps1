$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LerosRepoRoot
$runtimeState = Get-ConfiguredDevRuntimeState
$env:LEROS_DEV = 'true'
$workspaceMountRoot = "$root\.leros-workspace"

$resolvedWorkerConfig = New-ResolvedWorkerConfig -RepoRoot $root -ServerPort $runtimeState.serverPort

Set-Location $root
Write-Host "[Leros][Worker] Starting on http://localhost:$($runtimeState.workerPort)" -ForegroundColor Cyan
& "$root\bundles\leros.exe" worker --worker-id 1 --config $resolvedWorkerConfig --listen-addr ":$($runtimeState.workerPort)" --workspace-root $workspaceMountRoot

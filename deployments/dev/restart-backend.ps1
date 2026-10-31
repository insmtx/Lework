$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LeworkRepoRoot
$runtimeState = Initialize-DevRuntimeState

Stop-DevProcessesByPorts -Ports @([int]$runtimeState.serverPort, [int]$runtimeState.workerPort)

Write-Host '[Lework] Stopping remaining backend processes...' -ForegroundColor Cyan
Get-Process lework -ErrorAction SilentlyContinue | Stop-Process -Force

& "$PSScriptRoot\rebuild-backend.ps1"

$runtimeState = Get-ConfiguredDevRuntimeState
Prepare-DevRuntimeConfigs -RepoRoot $root -RuntimeState $runtimeState

Write-Host '[Lework] Restarting server and worker...' -ForegroundColor Cyan
Start-DevBackendWindows -RuntimeState $runtimeState

Write-Host ''
Write-Host '[Lework] Backend restart completed.' -ForegroundColor Green
Write-Host "[Lework] API server: http://localhost:$($runtimeState.serverPort)" -ForegroundColor Green
Write-Host "[Lework] Worker: http://localhost:$($runtimeState.workerPort)" -ForegroundColor Green
Write-Host '[Lework] Frontend does not need to restart. Just refresh the page.' -ForegroundColor Green
Read-Host 'Press Enter to exit'

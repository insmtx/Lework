$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LerosRepoRoot
$runtimeState = Initialize-DevRuntimeState

Stop-DevProcessesByPorts -Ports @([int]$runtimeState.serverPort, [int]$runtimeState.workerPort)

Write-Host '[Leros] Stopping remaining backend processes...' -ForegroundColor Cyan
Get-Process leros -ErrorAction SilentlyContinue | Stop-Process -Force

& "$PSScriptRoot\rebuild-backend.ps1"

Write-Host '[Leros] Restarting server and worker...' -ForegroundColor Cyan
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-server-dev.ps1" | Out-Null
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-worker-dev.ps1" | Out-Null

Write-Host ''
Write-Host '[Leros] Backend restart completed.' -ForegroundColor Green
Write-Host '[Leros] Frontend does not need to restart. Just refresh the page.' -ForegroundColor Green
Read-Host 'Press Enter to exit'

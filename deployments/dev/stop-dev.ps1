$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"

if (-not (Ensure-Administrator -ScriptPath "$PSScriptRoot\stop-dev.ps1")) {
    exit 0
}

$root = Get-LerosRepoRoot
$dockerExe = Get-DockerExe
$runtimeState = Get-DevRuntimeState
$portsToStop = @(3005, 8080, 8081)
if ($runtimeState) {
    $portsToStop += @([int]$runtimeState.serverPort, [int]$runtimeState.workerPort)
}

Stop-DevProcessesByPorts -Ports $portsToStop

Write-Host '[Leros] Stopping remaining backend processes...' -ForegroundColor Cyan
Get-Process leros -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host '[Leros] Stopping Postgres and NATS...' -ForegroundColor Cyan
& $dockerExe info *> $null
if ($LASTEXITCODE -eq 0) {
    & $dockerExe compose -f "$root\deployments\dev\docker-compose.dev.yml" down
} else {
    Write-Host '[Leros] Docker engine is not running, skip stopping containers.' -ForegroundColor Yellow
}

Write-Host ''
Write-Host '[Leros] Backend and Docker dependencies have been stopped.' -ForegroundColor Green
Write-Host '[Leros] Frontend on port 3005 has also been stopped if it was running.' -ForegroundColor Green
Read-Host 'Press Enter to exit'

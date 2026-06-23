$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LerosRepoRoot
$dockerDesktop = 'E:\DevEnv\Docker\app\Docker Desktop.exe'
$dockerExe = Get-DockerExe
$runtimeState = Initialize-DevRuntimeState

if (Get-Process leros -ErrorAction SilentlyContinue) {
    Write-Host '[Leros] Backend is already running.' -ForegroundColor Yellow
    Write-Host '[Leros] If you changed backend code, run deployments\dev\restart-backend.cmd' -ForegroundColor Yellow
    Read-Host 'Press Enter to exit'
    exit 0
}

if (Test-Path $dockerDesktop) {
    Start-Process -FilePath $dockerDesktop | Out-Null
}

Wait-DockerReady

Write-Host '[Leros] Starting Postgres and NATS...' -ForegroundColor Cyan
& $dockerExe compose -f "$root\deployments\dev\docker-compose.dev.yml" up -d postgresql nats
if ($LASTEXITCODE -ne 0) {
    throw 'Docker dependencies failed to start.'
}

if (-not (Test-Path "$root\bundles\leros.exe")) {
    & "$PSScriptRoot\rebuild-backend.ps1"
}

Write-Host "[Leros] Using API server port $($runtimeState.serverPort) and worker port $($runtimeState.workerPort)." -ForegroundColor Cyan

Write-Host '[Leros] Opening server, worker and frontend windows...' -ForegroundColor Cyan
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-server-dev.ps1" | Out-Null
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-worker-dev.ps1" | Out-Null
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-frontend-dev.ps1" | Out-Null

Write-Host ''
Write-Host '[Leros] Dev environment is ready.' -ForegroundColor Green
Write-Host '[Leros] Frontend auto refreshes. For backend changes, run deployments\dev\restart-backend.cmd' -ForegroundColor Green
Read-Host 'Press Enter to exit'

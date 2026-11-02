$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LeworkRepoRoot
$dockerDesktop = 'E:\DevEnv\Docker\app\Docker Desktop.exe'
$dockerExe = Get-DockerExe
$runtimeState = Initialize-DevRuntimeState

if (Get-Process lework -ErrorAction SilentlyContinue) {
    Write-Host '[Lework] Backend is already running.' -ForegroundColor Yellow
    Write-Host '[Lework] If you changed backend code, run deployments\dev\restart-backend.cmd' -ForegroundColor Yellow
    Read-Host 'Press Enter to exit'
    exit 0
}

if (Test-Path $dockerDesktop) {
    Start-Process -FilePath $dockerDesktop | Out-Null
}

Wait-DockerReady

Write-Host '[Lework] Starting Postgres and NATS...' -ForegroundColor Cyan
& $dockerExe compose -f "$root\deployments\dev\docker-compose.dev.yml" up -d postgresql nats
if ($LASTEXITCODE -ne 0) {
    throw 'Docker dependencies failed to start.'
}

Wait-DevPostgresReady -DockerExe $dockerExe
Wait-DevNatsReady -DockerExe $dockerExe
if (Test-DevDatabaseHasDuplicateUserOrgUin -DockerExe $dockerExe) {
    Write-Host '[Lework] Detected duplicate user-org UINs in the local development database.' -ForegroundColor Yellow
    Write-Host '[Lework] The current backend migration requires UIN to be unique.' -ForegroundColor Yellow
    $reset = Read-Host 'Reset local PostgreSQL/NATS volumes and recreate development data? (y/N)'
    if ($reset -match '^(y|yes)$') {
        & $dockerExe compose -f "$root\deployments\dev\docker-compose.dev.yml" down -v
        if ($LASTEXITCODE -ne 0) {
            throw 'Failed to reset local development data volumes.'
        }

        & $dockerExe compose -f "$root\deployments\dev\docker-compose.dev.yml" up -d postgresql nats
        if ($LASTEXITCODE -ne 0) {
            throw 'Failed to recreate local development dependencies.'
        }

        Wait-DevPostgresReady -DockerExe $dockerExe
        Wait-DevNatsReady -DockerExe $dockerExe
        Write-Host '[Lework] Local development data reset completed.' -ForegroundColor Green
    }
    else {
        throw 'Local database contains duplicate user-org UINs. Reset was cancelled.'
    }
}

Ensure-LatestBackendBinary -RepoRoot $root

Write-Host "[Lework] Using API server port $($runtimeState.serverPort) and worker port $($runtimeState.workerPort)." -ForegroundColor Cyan
Prepare-DevRuntimeConfigs -RepoRoot $root -RuntimeState $runtimeState

Start-DevBackendWindows -RuntimeState $runtimeState
Start-DevFrontendWindow

Write-Host ''
Write-Host '[Lework] Dev environment is ready.' -ForegroundColor Green
Write-Host "[Lework] Frontend: http://localhost:3005" -ForegroundColor Green
Write-Host "[Lework] API server: http://localhost:$($runtimeState.serverPort)" -ForegroundColor Green
Write-Host "[Lework] Worker: http://localhost:$($runtimeState.workerPort)" -ForegroundColor Green
Write-Host '[Lework] Frontend auto refreshes. For backend changes, run deployments\dev\restart-backend.cmd' -ForegroundColor Green
Read-Host 'Press Enter to exit'

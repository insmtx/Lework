$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LerosRepoRoot
$runtimeState = Initialize-DevRuntimeState
$dbPath = Get-WorkerRecoveryDbPath -RepoRoot $root
$workspaceRoot = Get-WorkerWorkspaceRoot -RepoRoot $root

Stop-DevProcessesByPorts -Ports @([int]$runtimeState.serverPort, [int]$runtimeState.workerPort)

Write-Host '[Leros] Stopping remaining backend processes...' -ForegroundColor Cyan
Get-Process leros -ErrorAction SilentlyContinue | Stop-Process -Force

$sqliteExe = Get-Sqlite3Exe

$dbPaths = New-Object 'System.Collections.Generic.List[string]'
if (Test-Path $dbPath) {
    $dbPaths.Add((Resolve-Path $dbPath).Path)
}
if (Test-Path $workspaceRoot) {
    Get-ChildItem -Path $workspaceRoot -Recurse -Filter 'leros.db' -ErrorAction SilentlyContinue | ForEach-Object {
        $resolvedPath = $_.FullName
        if (-not $dbPaths.Contains($resolvedPath)) {
            $dbPaths.Add($resolvedPath)
        }
    }
}

if ($dbPaths.Count -eq 0) {
    throw "Worker recovery database not found under: $workspaceRoot"
}

# 中文注释：本地 NATS 重建后 stream seq 可能从 1 重新开始，旧终态游标会把新消息误判为已处理。
$cleanupSql = @"
delete from task_seq
where topic = 'org.1.worker.1.task'
   or topic like 'org.1.worker.1.cmd.%';
"@
$countSql = @"
select topic || ': ' || count(*)
from task_seq
where topic = 'org.1.worker.1.task'
   or topic like 'org.1.worker.1.cmd.%'
group by topic;
"@

Write-Host '[Leros] Clearing worker task and command recovery state for chat stuck issue...' -ForegroundColor Cyan
foreach ($path in $dbPaths) {
    Write-Host "[Leros] Recovery DB: $path" -ForegroundColor DarkGray
    & $sqliteExe $path $cleanupSql
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to clear task_seq recovery state: $path"
    }

    # 中文注释：输出剩余记录，便于确认 task/cmd lane 的旧游标已经清空。
    $remainingRows = & $sqliteExe $path $countSql
    if ($remainingRows) {
        Write-Host "[Leros] Remaining recovery rows: $remainingRows" -ForegroundColor Yellow
    } else {
        Write-Host '[Leros] Remaining task/cmd recovery rows: 0' -ForegroundColor Green
    }
}

if (-not (Test-Path "$root\bundles\leros.exe")) {
    & "$PSScriptRoot\rebuild-backend.ps1"
}

Write-Host '[Leros] Restarting server and worker...' -ForegroundColor Cyan
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-server-dev.ps1" | Out-Null
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-worker-dev.ps1" | Out-Null

Write-Host ''
Write-Host '[Leros] Chat stuck recovery completed.' -ForegroundColor Green
Write-Host "[Leros] Workspace root: $workspaceRoot" -ForegroundColor DarkGray
Write-Host '[Leros] If the page is still generating forever, refresh once and retry the question.' -ForegroundColor Green
Read-Host 'Press Enter to exit'

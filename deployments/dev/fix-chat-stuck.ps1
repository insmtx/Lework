$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\shared.ps1"
Import-DevEnvFile

$root = Get-LeworkRepoRoot
$runtimeState = Initialize-DevRuntimeState
$dbPath = Get-WorkerRecoveryDbPath -RepoRoot $root
$workspaceRoot = Get-WorkerWorkspaceRoot -RepoRoot $root

Stop-DevProcessesByPorts -Ports @([int]$runtimeState.serverPort, [int]$runtimeState.workerPort)

Write-Host '[Lework] Stopping remaining backend processes...' -ForegroundColor Cyan
Get-Process lework -ErrorAction SilentlyContinue | Stop-Process -Force

$sqliteExe = Get-Sqlite3Exe

$dbPaths = New-Object 'System.Collections.Generic.List[string]'
if (Test-Path $dbPath) {
    $dbPaths.Add((Resolve-Path $dbPath).Path)
}
if (Test-Path $workspaceRoot) {
    Get-ChildItem -Path $workspaceRoot -Recurse -Filter 'lework.db' -ErrorAction SilentlyContinue | ForEach-Object {
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

Write-Host '[Lework] Clearing worker task and command recovery state for chat stuck issue...' -ForegroundColor Cyan
foreach ($path in $dbPaths) {
    Write-Host "[Lework] Recovery DB: $path" -ForegroundColor DarkGray
    & $sqliteExe $path $cleanupSql
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to clear task_seq recovery state: $path"
    }

    # 中文注释：输出剩余记录，便于确认 task/cmd lane 的旧游标已经清空。
    $remainingRows = & $sqliteExe $path $countSql
    if ($remainingRows) {
        Write-Host "[Lework] Remaining recovery rows: $remainingRows" -ForegroundColor Yellow
    } else {
        Write-Host '[Lework] Remaining task/cmd recovery rows: 0' -ForegroundColor Green
    }
}

Ensure-LatestBackendBinary -RepoRoot $root

Write-Host '[Lework] Restarting server and worker...' -ForegroundColor Cyan
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-server-dev.ps1" | Out-Null
Start-Sleep -Seconds 2
Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-worker-dev.ps1" | Out-Null

Write-Host ''
Write-Host '[Lework] Chat stuck recovery completed.' -ForegroundColor Green
Write-Host "[Lework] Workspace root: $workspaceRoot" -ForegroundColor DarkGray
Write-Host '[Lework] If the page is still generating forever, refresh once and retry the question.' -ForegroundColor Green
Read-Host 'Press Enter to exit'

$ErrorActionPreference = 'Stop'

$script:DevRuntimeStateFile = Join-Path $PSScriptRoot '.runtime-state.json'

function Get-LeworkRepoRoot {
    return (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
}

function Resolve-ToolPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$CommandName,

        [string[]]$FallbackPaths = @()
    )

    $command = Get-Command $CommandName -ErrorAction SilentlyContinue
    if ($command -and $command.Source) {
        return $command.Source
    }

    foreach ($path in $FallbackPaths) {
        if ($path -and (Test-Path $path)) {
            return $path
        }
    }

    throw "Required command not found: $CommandName"
}

function Get-DockerExe {
    return (Resolve-ToolPath -CommandName 'docker.exe' -FallbackPaths @(
        'E:\DevEnv\Docker\app\resources\bin\docker.exe'
    ))
}

function Get-GoExe {
    return (Resolve-ToolPath -CommandName 'go.exe' -FallbackPaths @(
        'E:\DevEnv\Go\goroot\bin\go.exe'
    ))
}

function Get-BackendBinaryPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    return Join-Path $RepoRoot 'bundles\lework.exe'
}

function Get-LatestBackendSourceWriteTimeUtc {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    $candidatePaths = @(
        (Join-Path $RepoRoot 'backend'),
        (Join-Path $RepoRoot 'go.mod'),
        (Join-Path $RepoRoot 'go.sum')
    )

    $latestWriteTime = [datetime]::MinValue
    foreach ($path in $candidatePaths) {
        if (-not (Test-Path $path)) {
            continue
        }

        $item = Get-Item $path
        if ($item.PSIsContainer) {
            $latestChild = Get-ChildItem -Path $path -Recurse -File -Include *.go |
                Sort-Object LastWriteTimeUtc -Descending |
                Select-Object -First 1
            if ($latestChild -and $latestChild.LastWriteTimeUtc -gt $latestWriteTime) {
                $latestWriteTime = $latestChild.LastWriteTimeUtc
            }
            continue
        }

        if ($item.LastWriteTimeUtc -gt $latestWriteTime) {
            $latestWriteTime = $item.LastWriteTimeUtc
        }
    }

    return $latestWriteTime
}

function Test-BackendBinaryNeedsRebuild {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    $binaryPath = Get-BackendBinaryPath -RepoRoot $RepoRoot
    if (-not (Test-Path $binaryPath)) {
        return $true
    }

    $binaryWriteTime = (Get-Item $binaryPath).LastWriteTimeUtc
    $sourceWriteTime = Get-LatestBackendSourceWriteTimeUtc -RepoRoot $RepoRoot
    return $sourceWriteTime -gt $binaryWriteTime
}

function Ensure-LatestBackendBinary {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    if (-not (Test-BackendBinaryNeedsRebuild -RepoRoot $RepoRoot)) {
        return
    }

    # 中文注释：开发脚本默认启动最新后端，避免静默复用旧 bundles 导致接口与源码不一致。
    Write-Host '[Lework] Backend source changed, rebuilding latest binary...' -ForegroundColor Cyan
    & (Join-Path $PSScriptRoot 'rebuild-backend.ps1')
}

function Get-PnpmExe {
    return (Resolve-ToolPath -CommandName 'pnpm.cmd' -FallbackPaths @(
        'D:\nvm\nodejs\pnpm.cmd'
    ))
}

function Get-Sqlite3Exe {
    return (Resolve-ToolPath -CommandName 'sqlite3')
}

function Set-OptionalGoBuildEnvironment {
    $fallbackGoroot = 'E:\DevEnv\Go\goroot'
    $fallbackGopath = 'E:\DevEnv\Go\gopath'
    $fallbackGocache = 'E:\DevEnv\Go\cache'
    $fallbackGcc = 'E:\DevEnv\MSYS2\ucrt64\bin\gcc.exe'
    $fallbackGccDir = 'E:\DevEnv\MSYS2\ucrt64\bin'

    if (-not $env:GOROOT -and (Test-Path $fallbackGoroot)) {
        $env:GOROOT = $fallbackGoroot
    }
    if (-not $env:GOPATH -and (Test-Path $fallbackGopath)) {
        $env:GOPATH = $fallbackGopath
    }
    if (-not $env:GOCACHE -and (Test-Path $fallbackGocache)) {
        $env:GOCACHE = $fallbackGocache
    }
    if (-not $env:GOMODCACHE -and $env:GOPATH) {
        $env:GOMODCACHE = Join-Path $env:GOPATH 'pkg\mod'
    }
    if (-not $env:GOBIN -and $env:GOPATH) {
        $env:GOBIN = Join-Path $env:GOPATH 'bin'
    }

    $env:CGO_ENABLED = '1'

    if (-not $env:CC -and (Test-Path $fallbackGcc)) {
        $env:CC = $fallbackGcc
    }

    if ((Test-Path $fallbackGccDir) -and ($env:PATH -notlike "*$fallbackGccDir*")) {
        $env:PATH = "$fallbackGccDir;$env:PATH"
    }

    if ($env:GOROOT) {
        $goBinDir = Join-Path $env:GOROOT 'bin'
        if ((Test-Path $goBinDir) -and ($env:PATH -notlike "*$goBinDir*")) {
            $env:PATH = "$goBinDir;$env:PATH"
        }
    }
}

function Wait-DockerReady {
    $dockerExe = Get-DockerExe

    Write-Host '[Lework] Waiting for Docker engine...' -ForegroundColor Cyan
    for ($i = 0; $i -lt 30; $i++) {
        & $dockerExe info *> $null
        if ($LASTEXITCODE -eq 0) {
            return
        }

        Start-Sleep -Seconds 2
    }

    throw 'Docker engine did not become ready in time.'
}

function Test-DevDatabaseHasDuplicateUserOrgUin {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DockerExe
    )

    $query = "SELECT COUNT(*) FROM (SELECT uin FROM lework_user_org GROUP BY uin HAVING COUNT(*) > 1) duplicates;"
    $duplicateCount = & $DockerExe exec lework-dev-postgresql psql -U lework_dev_user -d lework_dev_db -tAc $query 2>$null
    if ($LASTEXITCODE -ne 0) {
        return $false
    }

    $duplicateText = ($duplicateCount | Out-String).Trim()
    return ([int]$duplicateText -gt 0)
}

function Wait-DevPostgresReady {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DockerExe
    )

    for ($i = 0; $i -lt 30; $i++) {
        $health = & $DockerExe inspect --format '{{.State.Health.Status}}' lework-dev-postgresql 2>$null
        if ($LASTEXITCODE -eq 0 -and ($health | Out-String).Trim() -eq 'healthy') {
            return
        }

        Start-Sleep -Seconds 2
    }

    throw 'PostgreSQL did not become healthy in time.'
}

function Wait-DevNatsReady {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DockerExe
    )

    for ($i = 0; $i -lt 30; $i++) {
        $health = & $DockerExe inspect --format '{{.State.Health.Status}}' lework-dev-nats 2>$null
        if ($LASTEXITCODE -eq 0 -and ($health | Out-String).Trim() -eq 'healthy') {
            return
        }

        Start-Sleep -Seconds 2
    }

    throw 'NATS did not become healthy in time.'
}

function Wait-DevPortReady {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port,

        [Parameter(Mandatory = $true)]
        [string]$ServiceName,

        [int]$MaxAttempts = 60,

        [int]$IntervalSeconds = 2
    )

    for ($i = 0; $i -lt $MaxAttempts; $i++) {
        if (Test-PortListening -Port $Port) {
            return
        }

        Start-Sleep -Seconds $IntervalSeconds
    }

    throw "$ServiceName did not start listening on port $Port in time."
}

function Get-DefaultCLIConfigPath {
    return Join-Path $env:USERPROFILE '.lework\config.yaml'
}

function Get-DevNatsUrlFromConfig {
    $configPath = Join-Path $PSScriptRoot 'worker.config.yaml'
    if (-not (Test-Path $configPath)) {
        throw 'worker.config.yaml not found. Copy worker.config.example.yaml first.'
    }

    $content = Get-Content $configPath -Raw -Encoding UTF8
    if ($content -match '(?ms)^nats:\s*\r?\n\s*url:\s*["'']?([^"''\s]+)["'']?') {
        return $Matches[1]
    }

    throw 'NATS URL not found in worker.config.yaml.'
}

function Sync-DevCLIConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot,

        [Parameter(Mandatory = $true)]
        [int]$ServerPort
    )

    $resolvedWorkerConfig = New-ResolvedWorkerConfig -RepoRoot $RepoRoot -ServerPort $ServerPort
    $natsUrl = Get-DevNatsUrlFromConfig
    $cliConfigPath = Get-DefaultCLIConfigPath
    $cliDir = Split-Path $cliConfigPath -Parent
    $workspaceRoot = Join-Path $RepoRoot '.lework-workspace'

    if (-not (Test-Path $cliDir)) {
        New-Item -ItemType Directory -Path $cliDir | Out-Null
    }

    if (-not (Test-Path $cliConfigPath)) {
        # 中文注释：首次启动时写入 CLI 默认配置，避免调度子 Worker 回退到陈旧 NATS 地址。
        $content = Get-Content $resolvedWorkerConfig -Raw -Encoding UTF8
        if ($content -match '(?m)^workspace_root:\s*$') {
            $content = $content -replace '(?m)^workspace_root:\s*$', "workspace_root: $workspaceRoot"
        }
        Set-Content -Path $cliConfigPath -Value $content -Encoding UTF8
        Write-Host "[Lework] Created CLI config at $cliConfigPath" -ForegroundColor Cyan
        return
    }

    $inNatsBlock = $false
    $updatedLines = Get-Content $cliConfigPath -Encoding UTF8 | ForEach-Object {
        if ($_ -match '^\s*nats:\s*$') {
            $inNatsBlock = $true
            return $_
        }

        if ($inNatsBlock -and $_ -match '^(\s*url:\s*)') {
            $inNatsBlock = $false
            return $Matches[1] + $natsUrl
        }

        if ($_ -match '^\S') {
            $inNatsBlock = $false
        }

        if ($_ -match '^(\s*server_addr:\s*)') {
            return $Matches[1] + "127.0.0.1:$ServerPort"
        }

        return $_
    }

    Set-Content -Path $cliConfigPath -Value ($updatedLines -join [Environment]::NewLine) -Encoding UTF8
    Write-Host "[Lework] Synced NATS/server_addr in CLI config ($cliConfigPath)." -ForegroundColor Cyan
}

function ConvertTo-DevRuntimeHashtable {
    param(
        [Parameter(Mandatory = $true)]
        $State
    )

    if ($State -is [hashtable]) {
        return $State
    }

    return @{
        serverPort = [int]$State.serverPort
        workerPort = [int]$State.workerPort
        apiBaseUrl = [string]$State.apiBaseUrl
    }
}

function Prepare-DevRuntimeConfigs {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot,

        [Parameter(Mandatory = $true)]
        $RuntimeState
    )

    $state = ConvertTo-DevRuntimeHashtable -State $RuntimeState
    $null = New-ResolvedServerConfig -RepoRoot $RepoRoot -ServerPort $state.serverPort
    Sync-DevCLIConfig -RepoRoot $RepoRoot -ServerPort $state.serverPort
}

function Start-DevBackendWindows {
    param(
        [Parameter(Mandatory = $true)]
        $RuntimeState
    )

    $state = ConvertTo-DevRuntimeHashtable -State $RuntimeState

    Write-Host '[Lework] Opening server window...' -ForegroundColor Cyan
    Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-server-dev.ps1" | Out-Null
    Write-Host "[Lework] Waiting for server on port $($state.serverPort)..." -ForegroundColor Cyan
    Wait-DevPortReady -Port $state.serverPort -ServiceName 'API server'

    Write-Host '[Lework] Opening worker window...' -ForegroundColor Cyan
    Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-worker-dev.ps1" | Out-Null
    Write-Host "[Lework] Waiting for worker on port $($state.workerPort)..." -ForegroundColor Cyan
    Wait-DevPortReady -Port $state.workerPort -ServiceName 'Worker'
}

function Start-DevFrontendWindow {
    Write-Host '[Lework] Opening frontend window...' -ForegroundColor Cyan
    Start-Process powershell.exe -ArgumentList '-NoExit', '-ExecutionPolicy', 'Bypass', '-File', "$PSScriptRoot\run-frontend-dev.ps1" | Out-Null
    Write-Host '[Lework] Waiting for frontend on port 3005...' -ForegroundColor Cyan
    Wait-DevPortReady -Port 3005 -ServiceName 'Frontend' -MaxAttempts 90
}

function Import-DevEnvFile {
    $envPath = Join-Path $PSScriptRoot '.env'
    if (-not (Test-Path $envPath)) {
        return
    }

    Get-Content $envPath | ForEach-Object {
        $line = $_.Trim()
        if ($line -eq '' -or $line.StartsWith('#')) {
            return
        }

        $pair = $line.Split('=', 2)
        if ($pair.Length -ne 2) {
            return
        }

        $name = $pair[0].Trim()
        $value = $pair[1].Trim()
        if ($name -eq '') {
            return
        }

        [System.Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }
}

function Get-DevRuntimeState {
    if (-not (Test-Path $script:DevRuntimeStateFile)) {
        return $null
    }

    try {
        return Get-Content $script:DevRuntimeStateFile -Raw | ConvertFrom-Json
    } catch {
        return $null
    }
}

function Save-DevRuntimeState {
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State
    )

    $State | ConvertTo-Json | Set-Content -Path $script:DevRuntimeStateFile -Encoding UTF8
}

function Get-TcpExcludedPortRanges {
    $ranges = @()
    $output = netsh interface ipv4 show excludedportrange protocol=tcp
    foreach ($line in $output) {
        if ($line -match '^\s*(\d+)\s+(\d+)\s*(\*?)\s*$') {
            $ranges += [pscustomobject]@{
                StartPort = [int]$matches[1]
                EndPort   = [int]$matches[2]
            }
        }
    }

    return $ranges
}

function Test-PortListening {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    $listenRows = netstat -ano -p tcp |
        Select-String -Pattern 'LISTENING\s+\d+$' |
        Where-Object { $_.ToString() -match "[:\.]$Port\s" }

    return $listenRows.Count -gt 0
}

function Test-PortExcluded {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    foreach ($range in (Get-TcpExcludedPortRanges)) {
        if ($Port -ge $range.StartPort -and $Port -le $range.EndPort) {
            return $true
        }
    }

    return $false
}

function Test-PortUnavailable {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    return (Test-PortListening -Port $Port) -or (Test-PortExcluded -Port $Port)
}

function Resolve-DevPorts {
    $savedState = Get-DevRuntimeState
    if ($savedState -and $savedState.serverPort -and $savedState.workerPort) {
        if (-not (Test-PortExcluded -Port ([int]$savedState.serverPort)) -and
            -not (Test-PortExcluded -Port ([int]$savedState.workerPort))) {
            return @{
                ServerPort = [int]$savedState.serverPort
                WorkerPort = [int]$savedState.workerPort
            }
        }
    }

    # Windows may reserve 8080/8081, so dev scripts fall back to the next pair.
    $candidateServerPorts = @(8080, 18080, 28080, 38080)
    foreach ($serverPort in $candidateServerPorts) {
        $workerPort = $serverPort + 1
        if ((Test-PortUnavailable -Port $serverPort) -or (Test-PortUnavailable -Port $workerPort)) {
            continue
        }

        return @{
            ServerPort = $serverPort
            WorkerPort = $workerPort
        }
    }

    throw 'No available dev port pair found for server/worker.'
}

function Initialize-DevRuntimeState {
    $ports = Resolve-DevPorts
    $state = @{
        serverPort = $ports.ServerPort
        workerPort = $ports.WorkerPort
        apiBaseUrl = "http://localhost:$($ports.ServerPort)/v1"
    }
    Save-DevRuntimeState -State $state
    return $state
}

function Get-WorkerWorkspaceRoot {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    return Join-Path $RepoRoot '.lework-workspace'
}

function Get-WorkerRecoveryDbPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    return Join-Path (Get-WorkerWorkspaceRoot -RepoRoot $RepoRoot) '.lework\lework.db'
}

function Get-ConfiguredDevRuntimeState {
    $state = Get-DevRuntimeState
    if ($state) {
        return $state
    }

    return Initialize-DevRuntimeState
}

function New-ResolvedServerConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot,

        [Parameter(Mandatory = $true)]
        [int]$ServerPort
    )

    $runtimeDir = Join-Path $PSScriptRoot '.runtime'
    if (-not (Test-Path $runtimeDir)) {
        New-Item -ItemType Directory -Path $runtimeDir | Out-Null
    }

    $templatePath = Join-Path $PSScriptRoot 'server.config.yaml'
    $resolvedPath = Join-Path $runtimeDir 'server.config.runtime.yaml'
    # 中文注释：同步服务端口和调度地址，避免子 Worker 连接旧端口。
    $content = (
        Get-Content $templatePath -Encoding UTF8 | ForEach-Object {
            if ($_ -match '^(\s*port:\s*)\d+\s*$') {
                return $Matches[1] + $ServerPort
            }
            if ($_ -match '^(\s*server_addr:\s*).*$') {
                return $Matches[1] + "127.0.0.1:$ServerPort"
            }
            return $_
        }
    ) -join [Environment]::NewLine
    Set-Content -Path $resolvedPath -Value $content -Encoding UTF8
    return $resolvedPath
}

function New-ResolvedWorkerConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot,

        [Parameter(Mandatory = $true)]
        [int]$ServerPort
    )

    $runtimeDir = Join-Path $PSScriptRoot '.runtime'
    if (-not (Test-Path $runtimeDir)) {
        New-Item -ItemType Directory -Path $runtimeDir | Out-Null
    }

    $templatePath = Join-Path $PSScriptRoot 'worker.config.yaml'
    $resolvedPath = Join-Path $runtimeDir 'worker.config.runtime.yaml'
    # 中文注释：兼容本地配置中的单引号、双引号和无引号格式。
    $content = (
        Get-Content $templatePath -Encoding UTF8 | ForEach-Object {
            if ($_ -match '^(\s*server_addr:\s*).*$') {
                return $Matches[1] + '"127.0.0.1:' + $ServerPort + '"'
            }
            return $_
        }
    ) -join [Environment]::NewLine
    Set-Content -Path $resolvedPath -Value $content -Encoding UTF8
    return $resolvedPath
}

function Get-IsAdministrator {
    $currentIdentity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $currentPrincipal = New-Object System.Security.Principal.WindowsPrincipal($currentIdentity)
    return $currentPrincipal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Ensure-Administrator {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ScriptPath
    )

    if (Get-IsAdministrator) {
        return $true
    }

    Write-Host '[Lework] Re-launching with administrator permission...' -ForegroundColor Yellow
    Start-Process -FilePath 'powershell.exe' -ArgumentList @(
        '-ExecutionPolicy', 'Bypass',
        '-File', $ScriptPath
    ) -Verb RunAs | Out-Null

    return $false
}

function Stop-DevProcessesByPorts {
    param(
        [int[]]$Ports
    )

    $repoRoot = Get-LeworkRepoRoot
    $stoppedProcessIds = New-Object 'System.Collections.Generic.HashSet[int]'

    foreach ($port in $Ports) {
        $listenRows = netstat -ano -p tcp |
            Select-String -Pattern 'LISTENING\s+\d+$' |
            Where-Object { $_.ToString() -match "[:\.]$port\s" }

        foreach ($row in $listenRows) {
            $text = ($row.ToString() -replace '\s+', ' ').Trim()
            $parts = $text.Split(' ')
            if ($parts.Length -lt 5) {
                continue
            }

            $processId = $parts[-1]
            if ($processId -notmatch '^\d+$' -or $processId -eq '0') {
                continue
            }

            $pidValue = [int]$processId
            if ($stoppedProcessIds.Contains($pidValue)) {
                continue
            }

            $stoppedProcessIds.Add($pidValue) | Out-Null
            & taskkill /PID $pidValue /T /F *> $null

            if ($LASTEXITCODE -eq 0) {
                Write-Host "[Lework] Stopped process tree on port $port (PID: $pidValue)." -ForegroundColor Cyan
                continue
            }

            $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $pidValue" -ErrorAction SilentlyContinue
            if ($proc -and $proc.CommandLine -and $proc.CommandLine -match [regex]::Escape($repoRoot)) {
                throw "Failed to stop process on port $port. Please run stop script as administrator."
            }
        }
    }
}

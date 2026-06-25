$ErrorActionPreference = 'Stop'

$script:DevRuntimeStateFile = Join-Path $PSScriptRoot '.runtime-state.json'

function Get-LerosRepoRoot {
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

    Write-Host '[Leros] Waiting for Docker engine...' -ForegroundColor Cyan
    for ($i = 0; $i -lt 30; $i++) {
        & $dockerExe info *> $null
        if ($LASTEXITCODE -eq 0) {
            return
        }

        Start-Sleep -Seconds 2
    }

    throw 'Docker engine did not become ready in time.'
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

    return Join-Path $RepoRoot '.leros-workspace'
}

function Get-WorkerRecoveryDbPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )

    return Join-Path (Get-WorkerWorkspaceRoot -RepoRoot $RepoRoot) '.leros\leros.db'
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
    $content = Get-Content $templatePath -Raw
    # Only override the generated runtime config; keep the checked-in YAML unchanged.
    $content = [regex]::Replace(
        $content,
        '(?m)^(\s*port:\s*)\d+\s*$',
        [System.Text.RegularExpressions.MatchEvaluator]{ param($match) $match.Groups[1].Value + $ServerPort }
    )
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
    $content = Get-Content $templatePath -Raw
    # Keep the worker connected to the selected dev server port.
    $content = [regex]::Replace(
        $content,
        '(?m)^(\s*server_addr:\s*)".*"\s*$',
        [System.Text.RegularExpressions.MatchEvaluator]{ param($match) $match.Groups[1].Value + '"127.0.0.1:' + $ServerPort + '"' }
    )
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

    Write-Host '[Leros] Re-launching with administrator permission...' -ForegroundColor Yellow
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

    $repoRoot = Get-LerosRepoRoot
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
                Write-Host "[Leros] Stopped process tree on port $port (PID: $pidValue)." -ForegroundColor Cyan
                continue
            }

            $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $pidValue" -ErrorAction SilentlyContinue
            if ($proc -and $proc.CommandLine -and $proc.CommandLine -match [regex]::Escape($repoRoot)) {
                throw "Failed to stop process on port $port. Please run stop script as administrator."
            }
        }
    }
}

#!/usr/bin/env pwsh
#Requires -Version 5.1

Set-StrictMode -Version Latest

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
chcp 65001 | Out-Null

$ENDPOINT = "https://api.anysearch.com/mcp"
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Definition

function Load-Env {
    $envPaths = @(Join-Path $SCRIPT_DIR ".env", Join-Path $SCRIPT_DIR ".." ".env")
    foreach ($envPath in $envPaths) {
        if (Test-Path $envPath) {
            Get-Content $envPath -Encoding UTF8 | ForEach-Object {
                $line = $_.Split('#')[0].Trim()
                if ($line -and $line -match '=') {
                    $idx = $line.IndexOf('=')
                    $key = $line.Substring(0, $idx).Trim()
                    $val = $line.Substring($idx + 1).Trim().Trim('"').Trim("'")
                    if (-not (Test-Path "env:$key")) {
                        Set-Item -Path "env:$key" -Value $val
                    }
                }
            }
        }
    }
}

Load-Env

$AVAILABLE_DOMAINS = @(
    "code","tech","fashion","travel","home","ecommerce",
    "gaming","film","music","finance","academic","legal",
    "business","ip","security","education","health","religion",
    "geo","environment","energy","ugc"
)

$CONTENT_TYPES = @("web","news","code","doc","academic","data","image","video","audio")
$FRESHNESS_VALUES = @("day","week","month","year")
$ZONES = @("cn","intl")

function Call-Api {
    param(
        [string]$ToolName,
        [hashtable]$Arguments,
        [string]$ApiKey
    )

    $payload = @{
        jsonrpc = "2.0"
        id      = 1
        method  = "tools/call"
        params  = @{
            name      = $ToolName
            arguments = $Arguments
        }
    } | ConvertTo-Json -Depth 10 -Compress

    $headers = @{ "Content-Type" = "application/json; charset=utf-8" }
    if ($ApiKey) {
        $headers["Authorization"] = "Bearer $ApiKey"
    }

    try {
        $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
        $webReq = [System.Net.HttpWebRequest]::Create($ENDPOINT)
        $webReq.Method = "POST"
        $webReq.ContentType = "application/json; charset=utf-8"
        $webReq.Timeout = 30000
        if ($ApiKey) {
            $webReq.Headers.Add("Authorization", "Bearer $ApiKey")
        }
        $reqStream = $webReq.GetRequestStream()
        $reqStream.Write($bodyBytes, 0, $bodyBytes.Length)
        $reqStream.Close()
        $webResp = $webResp = $webReq.GetResponse()
        $respStream = $webResp.GetResponseStream()
        $respReader = New-Object System.IO.StreamReader($respStream, [System.Text.Encoding]::UTF8)
        $rawJson = $respReader.ReadToEnd()
        $respReader.Close()
        $webResp.Close()
        $resp = $rawJson | ConvertFrom-Json
    } catch {
        $err = $_.Exception.Message
        Write-Error "Connection Error: Unable to reach the API endpoint. ($err)"
        exit 1
    }

    $hasError = $false
    try { $hasError = ($null -ne $resp.error) } catch { }

    if ($hasError) {
        $errMsg = ""
        try { $errMsg = $resp.error.message } catch { $errMsg = $resp.error | ConvertTo-Json -Depth 5 }
        Write-Error "API Error: $errMsg"
        exit 1
    }

    $result = $null
    try { $result = $resp.result } catch { $result = $resp }

    if ($result -and $result.content) {
        foreach ($item in $result.content) {
            if ($item.type -eq "text") {
                return $item.text
            }
        }
    }
    return ($result | ConvertTo-Json -Depth 10)
}

function Parse-JsonList {
    param([string]$Value)
    try {
        $parsed = $Value | ConvertFrom-Json
        if ($parsed -is [array]) { return @($parsed) }
        return @($parsed)
    } catch {
        return @($Value -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    }
}

function Invoke-Search {
    param([hashtable]$Opts)

    $arguments = @{ query = $Opts.Query }

    if ($Opts.Domain) {
        $arguments["domain"] = $Opts.Domain
        if ($Opts.SubDomain) { $arguments["sub_domain"] = $Opts.SubDomain }
        if ($Opts.SubDomainParams) {
            try {
                $arguments["sub_domain_params"] = $Opts.SubDomainParams | ConvertFrom-Json -AsHashtable
            } catch {
                Write-Error "Error: --sub_domain_params must be valid JSON"
                exit 1
            }
        }
    }

    if ($Opts.ContentTypes) {
        $arguments["content_types"] = @(Parse-JsonList $Opts.ContentTypes)
    }
    if ($Opts.Zone) { $arguments["zone"] = $Opts.Zone }
    if ($Opts.MaxResults -ne $null) { $arguments["max_results"] = $Opts.MaxResults }
    if ($Opts.Freshness) { $arguments["freshness"] = $Opts.Freshness }

    $result = Call-Api -ToolName "search" -Arguments $arguments -ApiKey $Opts.ApiKey
    Write-Output $result
}

function Invoke-ListDomains {
    param([hashtable]$Opts)

    $arguments = @{}

    if ($Opts.Domains) {
        $arguments["domains"] = @(Parse-JsonList $Opts.Domains)
    } elseif ($Opts.Domain) {
        $arguments["domain"] = $Opts.Domain
    } else {
        Write-Error "Error: provide --domain or --domains"
        exit 1
    }

    $result = Call-Api -ToolName "list_domains" -Arguments $arguments -ApiKey $Opts.ApiKey
    Write-Output $result
}

function Invoke-Extract {
    param([hashtable]$Opts)

    if (-not $Opts.Url) {
        Write-Error "Error: url is required"
        exit 1
    }

    $arguments = @{ url = $Opts.Url }
    $result = Call-Api -ToolName "extract" -Arguments $arguments -ApiKey $Opts.ApiKey
    Write-Output $result
}

function Repair-Json {
    param([string]$Raw)

    $Raw = $Raw.Trim()
    if ($Raw.StartsWith('{') -and -not $Raw.StartsWith('[')) {
        $Raw = "[$Raw]"
    }
    if ($Raw.StartsWith('[')) {
        $inner = $Raw.Substring(1, $Raw.Length - 2).Trim()
        if (-not $inner) { return @() }
        $items = Split-JsonItems $inner
        $queries = @()
        foreach ($item in $items) {
            $item = $item.Trim().Trim(',')
            if (-not $item) { continue }
            if ($item.StartsWith('{')) {
                $queries += Repair-JsonObject $item
            } else {
                $queries += @{ query = $item.Trim().Trim("'").Trim('"') }
            }
        }
        return $queries
    }
    return @(@{ query = $Raw.Trim().Trim("'").Trim('"') })
}

function Split-JsonItems {
    param([string]$S)

    $depth = 0
    $current = ""
    $items = @()

    foreach ($ch in $S.ToCharArray()) {
        if ($ch -eq '{') { $depth++ }
        elseif ($ch -eq '}') { $depth-- }

        if ($ch -eq ',' -and $depth -eq 0) {
            $items += $current
            $current = ""
        } else {
            $current += $ch
        }
    }
    if ($current) {
        $tail = $current.Trim()
        if ($tail) { $items += $tail }
    }
    return ,$items
}

function Repair-JsonObject {
    param([string]$S)

    $inner = $S.Trim()
    if ($inner.StartsWith('{')) { $inner = $inner.Substring(1) }
    if ($inner.EndsWith('}')) { $inner = $inner.Substring(0, $inner.Length - 1) }
    $inner = $inner.Trim()
    if (-not $inner) { return @{} }

    $pairs = Split-JsonItems $inner
    $result = @{}

    foreach ($pair in $pairs) {
        $p = $pair.Trim().Trim(',')
        if (-not $p -or $p -notmatch ':') { continue }
        $colon = $p.IndexOf(':')
        $key = $p.Substring(0, $colon).Trim().Trim('"').Trim("'")
        $val = $p.Substring($colon + 1).Trim()

        if ($val.StartsWith('{')) {
            try { $result[$key] = $val | ConvertFrom-Json -AsHashtable }
            catch { $result[$key] = Repair-JsonObject $val }
        } elseif ($val.StartsWith('[')) {
            try { $result[$key] = @($val | ConvertFrom-Json) }
            catch { $result[$key] = @($val.Trim('[]') -split ',') }
        } elseif ($val -eq 'true') {
            $result[$key] = $true
        } elseif ($val -eq 'false') {
            $result[$key] = $false
        } elseif ($val -eq 'null') {
            $result[$key] = $null
        } else {
            try { $result[$key] = $val | ConvertFrom-Json }
            catch { $result[$key] = $val.Trim('"').Trim("'") }
        }
    }
    return $result
}

function Invoke-BatchSearch {
    param([hashtable]$Opts)

    $queries = $null

    if ($Opts.QueryItems -and $Opts.QueryItems.Count -gt 0) {
        if ($Opts.QueryItems.Count -gt 5) {
            Write-Error "Error: batch_search supports a maximum of 5 queries"
            exit 1
        }
        $queries = @($Opts.QueryItems | ForEach-Object { @{ query = $_ } })
    } elseif ($Opts.Queries) {
        $raw = $Opts.Queries
        if ($raw.StartsWith('@')) {
            $fpath = $raw.Substring(1)
            if (-not (Test-Path $fpath)) {
                Write-Error "Error: file not found: $fpath"
                exit 1
            }
            $raw = Get-Content $fpath -Raw -Encoding UTF8
        }
        try {
            $parsed = $raw | ConvertFrom-Json
            if ($parsed -is [array]) {
                $queries = @($parsed)
            } else {
                $queries = @($parsed)
            }
        } catch {
            $queries = Repair-Json $raw
        }
    } else {
        Write-Error "Error: provide --queries or --query"
        exit 1
    }

    $qcount = 0
    if ($queries) { $qcount = @($queries).Count }

    if ($qcount -lt 1) {
        Write-Error "Error: queries must contain at least 1 item"
        exit 1
    }
    if ($qcount -gt 5) {
        Write-Error "Error: batch_search supports a maximum of 5 queries"
        exit 1
    }

    $arguments = @{ queries = @($queries) }
    $result = Call-Api -ToolName "batch_search" -Arguments $arguments -ApiKey $Opts.ApiKey
    Write-Output $result
}

function Show-Doc {
    @'
# AnySearch Interface Specification (for AI Agent)

## Protocol
- Endpoint: POST https://api.anysearch.com/mcp
- Format: JSON-RPC 2.0, method = "tools/call"
- Auth: Header "Authorization: Bearer <API_KEY>" (optional, anonymous has lower rate limits)

## CLI Invocation (PowerShell)

```powershell
powershell -ExecutionPolicy Bypass -File <skill_dir>/scripts/anysearch_cli.ps1 <command> [options]
```

## Available Commands

### 1. search — Single query search
Two modes: general (omit --domain) and vertical (requires --domain + --sub_domain).

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| query | string | YES | Search query (positional). Vertical search MUST follow query_format from list_domains |
| --domain, -d | string | no | Vertical domain: code tech fashion travel home ecommerce gaming film music finance academic legal business ip security education health religion geo environment energy ugc |
| --sub_domain, -s | string | no | Sub-domain routing key (e.g. finance.us_stock). REQUIRED for vertical search |
| --sub_domain_params | JSON | no | Extra params per sub_domain schema from list_domains |
| --content_types, -t | string | no | Comma-separated or JSON array: web news code doc academic data image video audio |
| --zone, -z | string | no | cn / intl. Required when list_domains marks zone=CN |
| --max_results, -m | int | no | 1-100, default 10 |
| --freshness, -f | string | no | day / week / month / year |

### 2. list_domains — Query vertical domain directory
MUST be called before vertical search to discover available sub_domains and query formats.

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| --domain | string | choose one | Single domain to query |
| --domains | string | choose one | Batch up to 5 domains (comma-separated). Takes precedence over --domain |

Returns a Markdown table with columns: domain, sub_domain, description, query_format, params_schema, zone.

IMPORTANT: Cache list_domains results per domain within a session. Do NOT call repeatedly.

### 3. batch_search — Execute 2-5 search queries in parallel
Single failure does not block others; results are merged.

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| --query | string | YES (x1-5) | Repeatable single-query shorthand. Up to 5 |
| --queries, -q | JSON | YES | JSON array of query objects, or @file.json to read from file |

Each query object supports: query (required), domain, sub_domain, content_types, zone, max_results, freshness.

### 4. extract — Fetch full page content as Markdown
Truncated at 50,000 chars. HTML pages only.

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| url | string | YES | Target URL (positional or via --url / -u) |

---

## Decision Flow

```
User query
  |
  +-- Has structured identifiers? (Stock:/CVE:/DOI:/IATA:/patent etc.)
  |     YES -> 1) powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain X
  |             2) read query_format from result -> construct query accordingly
  |             3) powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "<query>" --domain X --sub_domain Y --zone cn
  |
  +-- Multiple independent intents?
  |     YES -> powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 batch_search --query "..." --query "..."
  |
  +-- Need deeper content than snippets?
        YES -> powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 extract "https://example.com/article"

  Otherwise -> powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "<general query>"
```

---

## Vertical Search Semantic Constraints

Before performing vertical search, you MUST call list_domains for the target domain
and strictly obey the returned semantic constraints:

1. **query_format**: Describes exactly how to structure the query string for that sub_domain.
   Example: "直接输入股票代码（如 AAPL）、公司名称、货币对（如 EUR_USD）、商品（如 WTICO_USD）"
   -> This means you pass the raw ticker/name/pair directly, NOT a natural language sentence.

2. **params_schema**: JSON schema for optional extra parameters.
   Example: {"type":"object","properties":{"period":{"type":"string","enum":["1d","1w","1m","3m","1y"]}}}
   -> You can pass --sub_domain_params '{"period":"1w"}' to narrow results.

3. **zone**: If "CN", you MUST set --zone cn in the search call.

4. **sub_domain selection**: Match the user's intent to the best sub_domain description.

---

## Scenario Examples (all runnable CLI commands)

### Scenario 1: General web search — look up a factual question

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "What is the capital of France"
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "quantum computing breakthroughs 2025" --max_results 5 --freshness month
```

### Scenario 2: Search with content type filter — find video or image results

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "how to bake sourdough bread" --content_types video --max_results 3
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "Mount Everest" --content_types image --max_results 5
```

### Scenario 3: Vertical search — stock market data (structured identifier)

Step 1: Discover available sub_domains for finance:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain finance
```

Step 2: Search with the correct sub_domain and query format:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "AAPL" --domain finance --sub_domain finance.us_stock --zone cn --max_results 5
```

### Scenario 4: Vertical search — academic paper lookup

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain academic
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "10.1038/s41586-020-2649-2" --domain academic --sub_domain academic.doi --max_results 3
```

### Scenario 5: Vertical search — security vulnerability (CVE)

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain security
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "CVE-2024-3094" --domain security --sub_domain security.cve --max_results 3
```

### Scenario 6: Vertical search — legal document or case

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain legal
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "contract dispute damages" --domain legal --sub_domain legal.case_law --max_results 5
```

### Scenario 7: Batch search — multiple independent queries in one call

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 batch_search --query "AAPL stock price" --query "TSLA earnings 2025" --query "GOOG market cap"
```

With full query objects (recommended for PowerShell to avoid quote-stripping issues):

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 batch_search --query AAPL --query TSLA --query GOOG
```

From a JSON file:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 batch_search --queries @queries.json
```

### Scenario 8: Extract full page content

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 extract "https://en.wikipedia.org/wiki/Quantum_computing"
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 extract --url "https://example.com/news/article-12345"
```

### Scenario 9: News search with time filter

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "AI regulation" --content_types news --freshness day --max_results 5
```

### Scenario 10: Search with API key

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "climate change policy 2025" --api_key <your_api_key> --max_results 3
```

### Scenario 11: China-specific vertical search (requires zone=cn)

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 list_domains --domain finance
```

```powershell
powershell -ExecutionPolicy Bypass -File scripts/anysearch_cli.ps1 search "600519" --domain finance --sub_domain finance.cn_stock --zone cn --max_results 5
```

---

## Rate Limit Handling
- On rate limit error with auto_registered api_key in response: present key to user for approval, then save to .env and retry
- On anonymous quota exhausted: inform user that a key provides higher limits; suggest configuring one via .env or environment variable
'@
}

function Show-Usage {
    @'
AnySearch CLI - Unified real-time search client.

Usage: anysearch.ps1 <command> [options]

Commands:
  search <query>         Search the web (general or vertical domain search)
  list_domains           Query domain directory for available sub_domains
  extract <url>          Fetch full page content from a URL
  batch_search           Execute 2-5 search queries in parallel
  doc                    Print AI-facing interface specification

Global Options:
  --api_key <key>        API key for authentication

Search Options:
  --domain, -d           Vertical domain
  --sub_domain, -s       Sub-domain routing key
  --sub_domain_params    Additional params as JSON
  --content_types, -t    Content filter (web,news,code,...)
  --zone, -z             Region: cn / intl
  --max_results, -m      Max results (default 10, max 100)
  --freshness, -f        Time filter: day/week/month/year

List-Domains Options:
  --domain               Single domain to query
  --domains              Batch domains (comma-separated or JSON array)

Batch-Search Options:
  --queries, -q          JSON array of query objects (or @file.json)
  --query                Repeatable single-query shorthand

Examples:
  .\anysearch.ps1 search "quantum computing"
  .\anysearch.ps1 search "AAPL" --domain finance --sub_domain finance.us_stock
  .\anysearch.ps1 list_domains --domain finance
  .\anysearch.ps1 extract https://example.com
  .\anysearch.ps1 batch_search --query AAPL --query GOOG
  .\anysearch.ps1 batch_search --queries '[{"query":"AAPL"},{"query":"GOOG"}]'
'@
}

$apiKey = if ($env:ANYSEARCH_API_KEY) { $env:ANYSEARCH_API_KEY } else { "" }

if ($args.Count -eq 0) {
    Show-Usage
    exit 0
}

$command = $args[0]
if ($args.Count -gt 1) {
    $rest = [array]$args[1..($args.Count - 1)]
} else {
    $rest = [array]@()
}

switch ($command) {
    "-h" { Show-Usage; exit 0 }
    "--help" { Show-Usage; exit 0 }
    "help" { Show-Usage; exit 0 }
}

switch ($command) {
    "search" {
        $query = ""
        $domain = ""
        $subDomain = ""
        $subDomainParams = ""
        $contentTypes = ""
        $zone = ""
        $maxResults = $null
        $freshness = ""

        $i = 0
        $positional = @()
        while ($i -lt $rest.Count) {
            if ($rest[$i] -match '^-') { break }
            $positional += $rest[$i]
            $i++
        }
        $query = $positional -join ' '

        while ($i -lt $rest.Count) {
            switch ($rest[$i]) {
                "--domain" { $domain = $rest[$i+1]; $i += 2 }
                "-d"       { $domain = $rest[$i+1]; $i += 2 }
                "--sub_domain" { $subDomain = $rest[$i+1]; $i += 2 }
                "-s"       { $subDomain = $rest[$i+1]; $i += 2 }
                "--sub_domain_params" { $subDomainParams = $rest[$i+1]; $i += 2 }
                "--content_types" { $contentTypes = $rest[$i+1]; $i += 2 }
                "-t"       { $contentTypes = $rest[$i+1]; $i += 2 }
                "--zone"   { $zone = $rest[$i+1]; $i += 2 }
                "-z"       { $zone = $rest[$i+1]; $i += 2 }
                "--max_results" { $maxResults = [int]$rest[$i+1]; $i += 2 }
                "-m"       { $maxResults = [int]$rest[$i+1]; $i += 2 }
                "--freshness" { $freshness = $rest[$i+1]; $i += 2 }
                "-f"       { $freshness = $rest[$i+1]; $i += 2 }
                "--api_key" { $apiKey = $rest[$i+1]; $i += 2 }
                default    { Write-Error "Unknown flag: $($rest[$i])"; exit 1 }
            }
        }

        if (-not $query) {
            Write-Error "Error: query is required"
            exit 1
        }

        Invoke-Search @{
            Query             = $query
            Domain            = $domain
            SubDomain         = $subDomain
            SubDomainParams   = $subDomainParams
            ContentTypes      = $contentTypes
            Zone              = $zone
            MaxResults        = $maxResults
            Freshness         = $freshness
            ApiKey            = $apiKey
        }
    }

    "list_domains" {
        $domain = ""
        $domains = ""

        $i = 0
        while ($i -lt $rest.Count) {
            switch ($rest[$i]) {
                "--domain"  { $domain = $rest[$i+1]; $i += 2 }
                "--domains" { $domains = $rest[$i+1]; $i += 2 }
                "--api_key" { $apiKey = $rest[$i+1]; $i += 2 }
                default     { Write-Error "Unknown flag: $($rest[$i])"; exit 1 }
            }
        }

        Invoke-ListDomains @{
            Domain = $domain
            Domains = $domains
            ApiKey  = $apiKey
        }
    }

    "extract" {
        $url = ""
        $positional = @()
        $i = 0

        while ($i -lt $rest.Count) {
            if ($rest[$i] -match '^-') { break }
            $positional += $rest[$i]
            $i++
        }
        $url = $positional -join ' '

        while ($i -lt $rest.Count) {
            switch ($rest[$i]) {
                "--url" { $url = $rest[$i+1]; $i += 2 }
                "-u"    { $url = $rest[$i+1]; $i += 2 }
                "--api_key" { $apiKey = $rest[$i+1]; $i += 2 }
                default { Write-Error "Unknown flag: $($rest[$i])"; exit 1 }
            }
        }

        Invoke-Extract @{ Url = $url; ApiKey = $apiKey }
    }

    "batch_search" {
        $queryItems = [System.Collections.Generic.List[string]]::new()
        $queries = $null
        $positional = $null
        $i = 0

        while ($i -lt $rest.Count) {
            switch ($rest[$i]) {
                "--queries" { $queries = $rest[$i+1]; $i += 2 }
                "-q"        { $queries = $rest[$i+1]; $i += 2 }
                "--query"   { $queryItems.Add($rest[$i+1]); $i += 2 }
                "--api_key" { $apiKey = $rest[$i+1]; $i += 2 }
                default     {
                    if (-not $positional) { $positional = $rest[$i] }
                    else { Write-Error "Unknown argument: $($rest[$i])"; exit 1 }
                    $i++
                }
            }
        }

        if ($positional -and -not $queries) { $queries = $positional }

        Invoke-BatchSearch @{
            Queries    = $queries
            QueryItems = $queryItems
            ApiKey     = $apiKey
        }
    }

    "doc" {
        Show-Doc
    }

    default {
        Write-Error "Unknown command: $command"
        Show-Usage
        exit 1
    }
}

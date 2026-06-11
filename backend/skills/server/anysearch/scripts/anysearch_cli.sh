#!/usr/bin/env bash
export LANG=en_US.UTF-8
export LC_ALL=en_US.UTF-8

ENDPOINT="https://api.anysearch.com/mcp"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

_load_env() {
  for env_path in "$SCRIPT_DIR/.env" "$SCRIPT_DIR/../.env"; do
    if [[ -f "$env_path" ]]; then
      while IFS= read -r line || [[ -n "$line" ]]; do
        line="${line%%#*}"
        line="$(echo "$line" | xargs 2>/dev/null || true)"
        [[ -z "$line" || "$line" != *=* ]] && continue
        local key="${line%%=*}"
        local val="${line#*=}"
        val="$(echo "$val" | sed 's/^["\x27]\|["\x27]$//g')"
        if [[ -z "${!key:-}" ]]; then
          export "$key=$val"
        fi
      done < "$env_path"
    fi
  done
}

_load_env

API_KEY="${ANYSEARCH_API_KEY:-}"

_json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

_json_value() {
  local json="$1"
  local key="$2"
  echo "$json" | grep -o "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | sed "s/\"$key\"[[:space:]]*:[[:space:]]*\"//;s/\"$//" | head -1
}

_json_value_raw() {
  local json="$1"
  local key="$2"
  echo "$json" | grep -o "\"$key\"[[:space:]]*:[[:space:]]*[^,}]*" | sed "s/\"$key\"[[:space:]]*:[[:space:]]*//" | head -1
}

_call_api() {
  local tool_name="$1"
  local arguments="$2"
  local auth_args=()
  if [[ -n "$API_KEY" ]]; then
    auth_args+=(-H "Authorization: Bearer $API_KEY")
  fi

  local escaped_tool
  escaped_tool=$(_json_escape "$tool_name")
  local payload="{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"$escaped_tool\",\"arguments\":$arguments}}"

  local response
  response=$(curl -s -X POST "$ENDPOINT" \
    -H "Content-Type: application/json" \
    "${auth_args[@]}" \
    -d "$payload" \
    --max-time 30 2>/dev/null)

  if [[ -z "$response" ]]; then
    echo "Error: No response from API" >&2
    exit 1
  fi

  local error_msg
  error_msg=$(_json_value "$response" "message")
  if [[ -n "$error_msg" && "$response" == *'"error"'* ]]; then
    echo "API Error: $error_msg" >&2
    exit 1
  fi

  if [[ "$response" == *'"result"'*'"content"'* ]]; then
    local text_block=""
    set +e
    text_block=$(echo "$response" | grep -o '"text":"[^"]*"' | head -1 | sed 's/"text":"//;s/"$//' 2>/dev/null)
    set -e
    if [[ -n "$text_block" ]]; then
      set +e
      echo "$text_block" | sed 's/\\n/\n/g; s/\\"/"/g; s/\\\\/\\/g'
      set -e
    else
      echo "$response"
    fi
  else
    echo "$response"
  fi
}

_cmd_search() {
  local query=""
  local domain=""
  local sub_domain=""
  local sub_domain_params=""
  local content_types=""
  local zone=""
  local max_results=""
  local freshness=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --domain|-d)     domain="$2"; shift 2 ;;
      --sub_domain|-s) sub_domain="$2"; shift 2 ;;
      --sub_domain_params) sub_domain_params="$2"; shift 2 ;;
      --content_types|-t) content_types="$2"; shift 2 ;;
      --zone|-z)       zone="$2"; shift 2 ;;
      --max_results|-m) max_results="$2"; shift 2 ;;
      --freshness|-f)  freshness="$2"; shift 2 ;;
      --api_key)       API_KEY="$2"; shift 2 ;;
      -*)              echo "Unknown flag: $1" >&2; _usage; exit 1 ;;
      *)               query="$1"; shift ;;
    esac
  done

  if [[ -z "$query" ]]; then
    echo "Error: query is required" >&2
    exit 1
  fi

  local escaped_query
  escaped_query=$(_json_escape "$query")
  local args="{\"query\":\"$escaped_query\"}"

  if [[ -n "$domain" ]]; then
    local escaped_domain
    escaped_domain=$(_json_escape "$domain")
    args="${args%\}},\"domain\":\"$escaped_domain\"}"
    if [[ -n "$sub_domain" ]]; then
      local escaped_sub
      escaped_sub=$(_json_escape "$sub_domain")
      args="${args%\}},\"sub_domain\":\"$escaped_sub\"}"
    fi
    if [[ -n "$sub_domain_params" ]]; then
      args="${args%\}},\"sub_domain_params\":$sub_domain_params}"
    fi
  fi

  if [[ -n "$content_types" ]]; then
    local ct
    if [[ "$content_types" == \[* ]]; then
      ct="$content_types"
    else
      ct="["
      local first=true
      IFS=',' read -ra items <<< "$content_types"
      for item in "${items[@]}"; do
        item="$(echo "$item" | xargs)"
        [[ -z "$item" ]] && continue
        [[ "$first" == "true" ]] && first=false || ct+=","
        ct+="\"$(_json_escape "$item")\""
      done
      ct+="]"
    fi
    args="${args%\}},\"content_types\":$ct}"
  fi

  if [[ -n "$zone" ]]; then
    local escaped_zone
    escaped_zone=$(_json_escape "$zone")
    args="${args%\}},\"zone\":\"$escaped_zone\"}"
  fi
  if [[ -n "$max_results" ]]; then
    args="${args%\}},\"max_results\":$max_results}"
  fi
  if [[ -n "$freshness" ]]; then
    local escaped_fresh
    escaped_fresh=$(_json_escape "$freshness")
    args="${args%\}},\"freshness\":\"$escaped_fresh\"}"
  fi

  _call_api "search" "$args"
}

_cmd_list_domains() {
  local domain=""
  local domains=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --domains)       domains="$2"; shift 2 ;;
      --domain)        domain="$2"; shift 2 ;;
      --api_key)       API_KEY="$2"; shift 2 ;;
      -*)              echo "Unknown flag: $1" >&2; exit 1 ;;
      *)               domain="$1"; shift ;;
    esac
  done

  local args="{}"
  if [[ -n "$domains" ]]; then
    local d
    if [[ "$domains" == \[* ]]; then
      d="$domains"
    else
      d="["
      local first=true
      IFS=',' read -ra items <<< "$domains"
      for item in "${items[@]}"; do
        item="$(echo "$item" | xargs)"
        [[ -z "$item" ]] && continue
        [[ "$first" == "true" ]] && first=false || d+=","
        d+="\"$(_json_escape "$item")\""
      done
      d+="]"
    fi
    args="{\"domains\":$d}"
  elif [[ -n "$domain" ]]; then
    local escaped_domain
    escaped_domain=$(_json_escape "$domain")
    args="{\"domain\":\"$escaped_domain\"}"
  else
    echo "Error: provide --domain or --domains" >&2
    exit 1
  fi

  _call_api "list_domains" "$args"
}

_cmd_extract() {
  local url=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --url|-u)        url="$2"; shift 2 ;;
      --api_key)       API_KEY="$2"; shift 2 ;;
      -*)              echo "Unknown flag: $1" >&2; exit 1 ;;
      *)               url="$1"; shift ;;
    esac
  done

  if [[ -z "$url" ]]; then
    echo "Error: url is required" >&2
    exit 1
  fi

  local escaped_url
  escaped_url=$(_json_escape "$url")
  local args="{\"url\":\"$escaped_url\"}"
  _call_api "extract" "$args"
}

_cmd_batch_search() {
  local queries=""
  local query_items=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --queries|-q)    queries="$2"; shift 2 ;;
      --query)         query_items+=("$2"); shift 2 ;;
      --api_key)       API_KEY="$2"; shift 2 ;;
      -*)              echo "Unknown flag: $1" >&2; exit 1 ;;
      *)               queries="$1"; shift ;;
    esac
  done

  local args
  if [[ ${#query_items[@]} -gt 0 ]]; then
    if [[ ${#query_items[@]} -gt 5 ]]; then
      echo "Error: batch_search supports a maximum of 5 queries" >&2
      exit 1
    fi
    local items="["
    for i in "${!query_items[@]}"; do
      [[ $i -gt 0 ]] && items+=","
      local escaped_q
      escaped_q=$(_json_escape "${query_items[$i]}")
      items+="{\"query\":\"$escaped_q\"}"
    done
    items+="]"
    args="{\"queries\":$items}"
  elif [[ -n "$queries" ]]; then
    local raw="$queries"
    if [[ "$raw" == @* ]]; then
      local fpath="${raw:1}"
      if [[ ! -f "$fpath" ]]; then
        echo "Error: file not found: $fpath" >&2
        exit 1
      fi
      raw=$(cat "$fpath")
    fi
    if [[ "$raw" == \[* || "$raw" == \{* ]]; then
      if [[ "$raw" == \[* ]]; then
        args="{\"queries\":$raw}"
      else
        args="{\"queries\":[$raw]}"
      fi
    else
      local items="["
      local first=true
      IFS=',' read -ra parts <<< "$raw"
      for part in "${parts[@]}"; do
        part="$(echo "$part" | xargs)"
        [[ -z "$part" ]] && continue
        [[ "$first" == "true" ]] && first=false || items+=","
        local escaped_q
        escaped_q=$(_json_escape "$part")
        items+="{\"query\":\"$escaped_q\"}"
      done
      items+="]"
      args="{\"queries\":$items}"
    fi
  else
    echo "Error: provide --queries or --query" >&2
    exit 1
  fi

  local count
  count=$(echo "$args" | grep -o '"query"' | wc -l)
  if [[ "$count" -lt 1 ]]; then
    echo "Error: queries must contain at least 1 item" >&2
    exit 1
  fi
  if [[ "$count" -gt 5 ]]; then
    echo "Error: batch_search supports a maximum of 5 queries" >&2
    exit 1
  fi

  _call_api "batch_search" "$args"
}

_cmd_doc() {
  cat <<'DOCEOF'
# AnySearch Interface Specification (for AI Agent)

## Protocol
- Endpoint: POST https://api.anysearch.com/mcp
- Format: JSON-RPC 2.0, method = "tools/call"
- Auth: Header "Authorization: Bearer <API_KEY>" (optional, anonymous has lower rate limits)

## CLI Invocation (Bash)

```bash
bash <skill_dir>/scripts/anysearch_cli.sh <command> [options]
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
  |     YES -> 1) bash scripts/anysearch_cli.sh list_domains --domain X
  |             2) read query_format from result -> construct query accordingly
  |             3) bash scripts/anysearch_cli.sh search "<query>" --domain X --sub_domain Y --zone cn
  |
  +-- Multiple independent intents?
  |     YES -> bash scripts/anysearch_cli.sh batch_search --query "..." --query "..."
  |
  +-- Need deeper content than snippets?
        YES -> bash scripts/anysearch_cli.sh extract "https://example.com/article"

  Otherwise -> bash scripts/anysearch_cli.sh search "<general query>"
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

```bash
bash scripts/anysearch_cli.sh search "What is the capital of France"
```

```bash
bash scripts/anysearch_cli.sh search "quantum computing breakthroughs 2025" --max_results 5 --freshness month
```

### Scenario 2: Search with content type filter — find video or image results

```bash
bash scripts/anysearch_cli.sh search "how to bake sourdough bread" --content_types video --max_results 3
```

```bash
bash scripts/anysearch_cli.sh search "Mount Everest" --content_types image --max_results 5
```

### Scenario 3: Vertical search — stock market data (structured identifier)

Step 1: Discover available sub_domains for finance:

```bash
bash scripts/anysearch_cli.sh list_domains --domain finance
```

Step 2: Search with the correct sub_domain and query format:

```bash
bash scripts/anysearch_cli.sh search "AAPL" --domain finance --sub_domain finance.us_stock --zone cn --max_results 5
```

### Scenario 4: Vertical search — academic paper lookup

```bash
bash scripts/anysearch_cli.sh list_domains --domain academic
```

```bash
bash scripts/anysearch_cli.sh search "10.1038/s41586-020-2649-2" --domain academic --sub_domain academic.doi --max_results 3
```

### Scenario 5: Vertical search — security vulnerability (CVE)

```bash
bash scripts/anysearch_cli.sh list_domains --domain security
```

```bash
bash scripts/anysearch_cli.sh search "CVE-2024-3094" --domain security --sub_domain security.cve --max_results 3
```

### Scenario 6: Vertical search — legal document or case

```bash
bash scripts/anysearch_cli.sh list_domains --domain legal
```

```bash
bash scripts/anysearch_cli.sh search "contract dispute damages" --domain legal --sub_domain legal.case_law --max_results 5
```

### Scenario 7: Batch search — multiple independent queries in one call

```bash
bash scripts/anysearch_cli.sh batch_search --query "AAPL stock price" --query "TSLA earnings 2025" --query "GOOG market cap"
```

With full query objects:

```bash
bash scripts/anysearch_cli.sh batch_search --queries '[{"query":"AAPL","domain":"finance","sub_domain":"finance.us_stock"},{"query":"python async http","domain":"code","sub_domain":"code.general"}]'
```

From a JSON file:

```bash
bash scripts/anysearch_cli.sh batch_search --queries @queries.json
```

### Scenario 8: Extract full page content

```bash
bash scripts/anysearch_cli.sh extract "https://en.wikipedia.org/wiki/Quantum_computing"
```

```bash
bash scripts/anysearch_cli.sh extract --url "https://example.com/news/article-12345"
```

### Scenario 9: News search with time filter

```bash
bash scripts/anysearch_cli.sh search "AI regulation" --content_types news --freshness day --max_results 5
```

### Scenario 10: Search with API key

```bash
bash scripts/anysearch_cli.sh search "climate change policy 2025" --api_key <your_api_key> --max_results 3
```

### Scenario 11: China-specific vertical search (requires zone=cn)

```bash
bash scripts/anysearch_cli.sh list_domains --domain finance
```

```bash
bash scripts/anysearch_cli.sh search "600519" --domain finance --sub_domain finance.cn_stock --zone cn --max_results 5
```

---

## Rate Limit Handling
- On rate limit error with auto_registered api_key in response: present key to user for approval, then save to .env and retry
- On anonymous quota exhausted: inform user that a key provides higher limits; suggest configuring one via .env or environment variable
DOCEOF
}

_usage() {
  cat <<'USAGE'
AnySearch CLI - Unified real-time search client.

Usage: anysearch.sh <command> [options]

Commands:
  search <query>         Search the web (general or vertical domain search)
  list_domains           Query domain directory for available sub_domains
  extract <url>          Fetch full page content from a URL
  batch_search           Execute 2-5 search queries in parallel
  doc                    Print AI-facing interface specification

Global Options:
  --api_key <key>        API key for authentication

Search Options:
  --domain, -d           Vertical domain (code/tech/finance/...)
  --sub_domain, -s       Sub-domain routing key
  --sub_domain_params    Additional params as JSON
  --content_types, -t    Content filter (web,news,code,...)
  --zone, -z             Region: cn / intl
  --max_results, -m      Max results (default 10, max 100)
  --freshness, -f        Time filter: day/week/month/yea

List-Domains Options:
  --domain               Single domain to query
  --domains              Batch domains (comma-separated or JSON array)

Batch-Search Options:
  --queries, -q          JSON array of query objects (or @file.json)
  --query                Repeatable single-query shorthand

Examples:
  anysearch.sh search "quantum computing"
  anysearch.sh search "AAPL" --domain finance --sub_domain finance.us_stock
  anysearch.sh list_domains --domain finance
  anysearch.sh extract https://example.com
  anysearch.sh batch_search --query AAPL --query GOOG
  anysearch.sh batch_search --queries '[{"query":"AAPL"},{"query":"GOOG"}]'
USAGE
}

main() {
  local command="${1:-}"
  shift || true

  case "$command" in
    search)         _cmd_search "$@" ;;
    list_domains)   _cmd_list_domains "$@" ;;
    extract)        _cmd_extract "$@" ;;
    batch_search)   _cmd_batch_search "$@" ;;
    doc)            _cmd_doc ;;
    -h|--help|help) _usage ;;
    "")             _usage ;;
    *)              echo "Unknown command: $command" >&2; _usage; exit 1 ;;
  esac
}

main "$@"

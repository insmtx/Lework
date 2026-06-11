#!/usr/bin/env python3
"""AnySearch CLI - Unified search client for AnySearch API."""

import argparse
import io
import json
import os
import sys
import requests

if sys.stdout.encoding != "utf-8":
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
if sys.stderr.encoding != "utf-8":
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

ENDPOINT = "https://api.anysearch.com/mcp"

def _load_env():
    """Load API keys from .env files near the skill.

    The documented priority is:
    --api_key > .env file > environment variable > anonymous.

    Use utf-8-sig so .env files saved by Windows Notepad with a BOM are parsed
    correctly. The .env value intentionally overrides an existing environment
    variable to match the documented priority order.
    """
    script_dir = os.path.dirname(os.path.abspath(__file__))
    for env_path in [os.path.join(script_dir, ".env"), os.path.join(script_dir, "..", ".env")]:
        if os.path.isfile(env_path):
            with open(env_path, "r", encoding="utf-8-sig") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#"):
                        continue
                    if "=" not in line:
                        continue
                    key, _, value = line.partition("=")
                    key = key.strip().lstrip(chr(0xFEFF))
                    value = value.strip().strip("\"'").strip()
                    if key and value:
                        os.environ[key] = value


_load_env()


AVAILABLE_DOMAINS = [
    "code", "tech", "fashion", "travel", "home", "ecommerce",
    "gaming", "film", "music", "finance", "academic", "legal",
    "business", "ip", "security", "education", "health", "religion",
    "geo", "environment", "energy", "ugc",
]

CONTENT_TYPES = [
    "web", "news", "code", "doc", "academic",
    "data", "image", "video", "audio",
]

FRESHNESS_VALUES = ["day", "week", "month", "year"]
ZONES = ["cn", "intl"]


def _build_headers(api_key: str) -> dict:
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    return headers

def _call_api(tool_name: str, arguments: dict, api_key: str) -> str:
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {"name": tool_name, "arguments": arguments},
    }
    try:
        resp = requests.post(ENDPOINT, json=payload, headers=_build_headers(api_key), timeout=30)
        resp.raise_for_status()
    except requests.exceptions.HTTPError as e:
        print(f"HTTP Error: {e}", file=sys.stderr)
        try:
            detail = resp.json()
            print(f"Response: {json.dumps(detail, ensure_ascii=False)}", file=sys.stderr)
        except Exception:
            print(f"Response body: {resp.text[:500]}", file=sys.stderr)
        sys.exit(1)
    except requests.exceptions.ConnectionError:
        print("Connection Error: Unable to reach the API endpoint.", file=sys.stderr)
        sys.exit(1)
    except requests.exceptions.Timeout:
        print("Timeout: The API request timed out.", file=sys.stderr)
        sys.exit(1)

    data = resp.json()
    if "error" in data:
        error_msg = data["error"].get("message", str(data["error"]))
        print(f"API Error: {error_msg}", file=sys.stderr)
        sys.exit(1)
    result = data.get("result", {})
    content = result.get("content", [])
    for item in content:
        if item.get("type") == "text":
            return item.get("text", "")
    return json.dumps(result, indent=2, ensure_ascii=False)


def _parse_json_list(value: str) -> list:
    try:
        parsed = json.loads(value)
        if isinstance(parsed, list):
            return parsed
        return [parsed]
    except json.JSONDecodeError:
        return [s.strip() for s in value.split(",") if s.strip()]


def cmd_search(args):
    """Execute search (general or vertical)."""
    arguments = {"query": args.query}

    if args.domain:
        arguments["domain"] = args.domain
        if args.sub_domain:
            arguments["sub_domain"] = args.sub_domain
        if args.sub_domain_params:
            try:
                arguments["sub_domain_params"] = json.loads(args.sub_domain_params)
            except json.JSONDecodeError:
                print("Error: --sub_domain_params must be valid JSON", file=sys.stderr)
                sys.exit(1)

    if args.content_types:
        arguments["content_types"] = _parse_json_list(args.content_types)
    if args.zone:
        arguments["zone"] = args.zone
    if args.max_results is not None:
        arguments["max_results"] = args.max_results
    if args.freshness:
        arguments["freshness"] = args.freshness

    print(_call_api("search", arguments, args.api_key))


def cmd_list_domains(args):
    """List available sub_domains for given domain(s)."""
    arguments = {}
    if args.domains:
        arguments["domains"] = _parse_json_list(args.domains)
    elif args.domain:
        arguments["domain"] = args.domain
    else:
        print("Error: provide --domain or --domains", file=sys.stderr)
        sys.exit(1)

    print(_call_api("list_domains", arguments, args.api_key))


def cmd_extract(args):
    """Fetch and extract full page content from a URL."""
    url = args.url or getattr(args, "url_opt", None)
    if not url:
        print("Error: url is required", file=sys.stderr)
        sys.exit(1)
    arguments = {"url": url}
    print(_call_api("extract", arguments, args.api_key))


def _repair_json(raw: str) -> list:
    raw = raw.strip()
    if raw.startswith("{") and not raw.startswith("["):
        raw = "[" + raw + "]"
    if raw.startswith("["):
        content = raw.strip("[]")
        if not content:
            return []
        items = _split_json_items(content)
        queries = []
        for item in items:
            item = item.strip().strip(",")
            if not item:
                continue
            if item.startswith("{"):
                d = _repair_json_object(item)
                queries.append(d)
            else:
                s = item.strip().strip("'\"")
                queries.append({"query": s})
        return queries
    return [{"query": raw.strip().strip("'\"")}]


def _split_json_items(s: str) -> list:
    depth = 0
    current = []
    items = []
    for ch in s:
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
        if ch == "," and depth == 0:
            items.append("".join(current))
            current = []
        else:
            current.append(ch)
    if current:
        tail = "".join(current).strip()
        if tail:
            items.append(tail)
    return items


def _repair_json_object(s: str) -> dict:
    inner = s.strip().strip("{}").strip()
    if not inner:
        return {}
    pairs = _split_json_items(inner)
    result = {}
    for pair in pairs:
        pair = pair.strip().strip(",")
        if not pair:
            continue
        if ":" not in pair:
            continue
        colon = pair.index(":")
        key = pair[:colon].strip().strip("'\"")
        val = pair[colon + 1:].strip()
        if val.startswith("{"):
            try:
                result[key] = json.loads(val)
            except json.JSONDecodeError:
                result[key] = _repair_json_object(val)
        elif val.startswith("["):
            try:
                result[key] = json.loads(val)
            except json.JSONDecodeError:
                result[key] = val.strip("[]").split(",")
        elif val.lower() in ("true", "false"):
            result[key] = val.lower() == "true"
        elif val.lower() == "null":
            result[key] = None
        else:
            try:
                result[key] = json.loads(val)
            except (json.JSONDecodeError, ValueError):
                result[key] = val.strip("'\"")
    return result


def cmd_batch_search(args):
    """Execute multiple search queries in parallel (2-5 queries)."""
    query_items = getattr(args, "query_items", None) or []
    raw = args.queries or getattr(args, "queries_opt", None)

    if query_items:
        queries = [{"query": q} for q in query_items]
        if len(queries) > 5:
            print("Error: batch_search supports a maximum of 5 queries", file=sys.stderr)
            sys.exit(1)
    elif raw:
        if raw.startswith("@"):
            file_path = raw[1:]
            try:
                with open(file_path, "r", encoding="utf-8") as f:
                    raw = f.read()
            except FileNotFoundError:
                print(f"Error: file not found: {file_path}", file=sys.stderr)
                sys.exit(1)
        try:
            queries = json.loads(raw)
            if not isinstance(queries, list):
                queries = [queries]
        except json.JSONDecodeError:
            queries = _repair_json(raw)
        if len(queries) < 1:
            print("Error: queries must contain at least 1 item", file=sys.stderr)
            sys.exit(1)
        if len(queries) > 5:
            print("Error: batch_search supports a maximum of 5 queries", file=sys.stderr)
            sys.exit(1)
    else:
        print("Error: provide --queries or --query", file=sys.stderr)
        sys.exit(1)

    arguments = {"queries": queries}
    print(_call_api("batch_search", arguments, args.api_key))


DOC_SPEC = """\
# AnySearch Interface Specification (for AI Agent)

## Protocol
- Endpoint: POST https://api.anysearch.com/mcp
- Format: JSON-RPC 2.0, method = "tools/call"
- Auth: Header "Authorization: Bearer <API_KEY>" (optional, anonymous has lower rate limits)

## CLI Invocation (Python)

```
python <skill_dir>/scripts/anysearch_cli.py <command> [options]
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
  |     YES -> 1) python scripts/anysearch_cli.py list_domains --domain X
  |             2) read query_format from result -> construct query accordingly
  |             3) python scripts/anysearch_cli.py search "<query>" --domain X --sub_domain Y --zone cn
  |
  +-- Multiple independent intents?
  |     YES -> python scripts/anysearch_cli.py batch_search --query "..." --query "..."
  |
  +-- Need deeper content than snippets?
        YES -> python scripts/anysearch_cli.py extract "https://example.com/article"

  Otherwise -> python scripts/anysearch_cli.py search "<general query>"
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
   Example: for "AAPL earnings report", prefer finance.us_stock over finance.forex.

---

## Scenario Examples (all runnable CLI commands)

### Scenario 1: General web search — look up a factual question

```bash
python scripts/anysearch_cli.py search "What is the capital of France"
```

```bash
python scripts/anysearch_cli.py search "quantum computing breakthroughs 2025" --max_results 5 --freshness month
```

### Scenario 2: Search with content type filter — find video or image results

```bash
python scripts/anysearch_cli.py search "how to bake sourdough bread" --content_types video --max_results 3
```

```bash
python scripts/anysearch_cli.py search "Mount Everest" --content_types image --max_results 5
```

### Scenario 3: Vertical search — stock market data (structured identifier)

Step 1: Discover available sub_domains for finance:

```bash
python scripts/anysearch_cli.py list_domains --domain finance
```

Step 2: Search with the correct sub_domain and query format (e.g. US stock):

```bash
python scripts/anysearch_cli.py search "AAPL" --domain finance --sub_domain finance.us_stock --zone cn --max_results 5
```

### Scenario 4: Vertical search — academic paper lookup

Step 1: Discover sub_domains for academic:

```bash
python scripts/anysearch_cli.py list_domains --domain academic
```

Step 2: Search by DOI:

```bash
python scripts/anysearch_cli.py search "10.1038/s41586-020-2649-2" --domain academic --sub_domain academic.doi --max_results 3
```

### Scenario 5: Vertical search — security vulnerability (CVE)

```bash
python scripts/anysearch_cli.py list_domains --domain security
```

```bash
python scripts/anysearch_cli.py search "CVE-2024-3094" --domain security --sub_domain security.cve --max_results 3
```

### Scenario 6: Vertical search — legal document or case

```bash
python scripts/anysearch_cli.py list_domains --domain legal
```

```bash
python scripts/anysearch_cli.py search "contract dispute damages" --domain legal --sub_domain legal.case_law --max_results 5
```

### Scenario 7: Vertical search — code search

```bash
python scripts/anysearch_cli.py search "python async http client" --domain code --sub_domain code.general --max_results 5
```

### Scenario 8: Batch search — multiple independent queries in one call

```bash
python scripts/anysearch_cli.py batch_search --query "AAPL stock price" --query "TSLA earnings 2025" --query "GOOG market cap"
```

With full query objects (vertical domain + parameters):

```bash
python scripts/anysearch_cli.py batch_search --queries '[{"query":"AAPL","domain":"finance","sub_domain":"finance.us_stock","zone":"cn"},{"query":"python async http","domain":"code","sub_domain":"code.general"}]'
```

From a JSON file:

```bash
python scripts/anysearch_cli.py batch_search --queries @queries.json
```

### Scenario 9: Extract full page content — read beyond search snippets

```bash
python scripts/anysearch_cli.py extract "https://en.wikipedia.org/wiki/Quantum_computing"
```

```bash
python scripts/anysearch_cli.py extract --url "https://example.com/news/article-12345"
```

### Scenario 10: News search with time filter

```bash
python scripts/anysearch_cli.py search "AI regulation" --content_types news --freshness day --max_results 5
```

### Scenario 11: Search with API key

```bash
python scripts/anysearch_cli.py search "climate change policy 2025" --api_key <your_api_key> --max_results 3
```

### Scenario 12: China-specific vertical search (requires zone=cn)

```bash
python scripts/anysearch_cli.py list_domains --domain finance
```

```bash
python scripts/anysearch_cli.py search "600519" --domain finance --sub_domain finance.cn_stock --zone cn --max_results 5
```

---

## Rate Limit Handling
- On rate limit error with auto_registered api_key in response: present key to user for approval, then save to .env and retry
- On anonymous quota exhausted: inform user that a key provides higher limits; suggest configuring one via .env or environment variable
"""


def cmd_doc(args):
    print(DOC_SPEC)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="anysearch",
        description=(
            "AnySearch CLI - Unified real-time search client.\n\n"
            "Supports general search, vertical domain search, batch search,\n"
            "domain directory lookup, and URL content extraction via the\n"
            "AnySearch JSON-RPC API."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "examples:\n"
            "  anysearch search \"quantum computing\"\n"
            "  anysearch search \"AAPL\" --domain finance --sub_domain finance.us_stock\n"
            "  anysearch list_domains --domain finance\n"
            "  anysearch extract --url https://example.com\n"
            "  anysearch batch_search --queries '[{\"query\":\"AAPL\"},{\"query\":\"GOOG\"}]'\n"
        ),
    )

    parser.add_argument(
        "--api_key",
        default=os.environ.get("ANYSEARCH_API_KEY", ""),
        help="API key for authentication. Read from: --api_key > .env ANYSEARCH_API_KEY > env ANYSEARCH_API_KEY. "
        "Without a key, anonymous access is used with lower rate limits.",
    )

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    search_p = subparsers.add_parser(
        "search",
        help="Search the web (general or vertical domain search)",
        description=(
            "Execute a search query.\n\n"
            "Two modes:\n"
            "  General search:   omit --domain (open-ended natural language queries)\n"
            "  Vertical search:  specify --domain and --sub_domain for structured queries\n\n"
            "For vertical search, run 'list_domains' first to discover available\n"
            "sub_domains and their required query formats."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    search_p.add_argument("query", help="Search query string. For vertical search, follow the format returned by list_domains.")
    search_p.add_argument(
        "--domain", "-d",
        choices=AVAILABLE_DOMAINS,
        help=(
            "Vertical domain for structured search. "
            f"Available: {', '.join(AVAILABLE_DOMAINS)}"
        ),
    )
    search_p.add_argument(
        "--sub_domain", "-s",
        help="Sub-domain routing key (e.g. finance.us_stock). Required for vertical search; obtain via list_domains.",
    )
    search_p.add_argument(
        "--sub_domain_params",
        help="Additional sub_domain parameters as JSON string. Schema depends on the sub_domain (see list_domains output).",
    )
    search_p.add_argument(
        "--content_types", "-t",
        help=(
            "Content type filter(s). Comma-separated or JSON array.\n"
            f"Available: {', '.join(CONTENT_TYPES)}"
        ),
    )
    search_p.add_argument(
        "--zone", "-z",
        choices=ZONES,
        help="Region zone: 'cn' for China, 'intl' for international. Required when list_domains marks CN.",
    )
    search_p.add_argument(
        "--max_results", "-m",
        type=int,
        help="Maximum number of results to return (default 10, max 100).",
    )
    search_p.add_argument(
        "--freshness", "-f",
        choices=FRESHNESS_VALUES,
        help="Time-based filter: day, week, month, year.",
    )
    search_p.set_defaults(func=cmd_search)

    ld_p = subparsers.add_parser(
        "list_domains",
        help="Query domain directory for available sub_domains",
        description=(
            "List available sub_domains, query formats, and parameter schemas\n"
            "for one or more vertical domains.\n\n"
            "MUST be called before performing vertical search to obtain\n"
            "the correct sub_domain value and query_format.\n\n"
            "Results are returned as a Markdown table with columns:\n"
            "domain, sub_domain, description, query_format, params_schema, zone."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    ld_p.add_argument(
        "--domain",
        choices=AVAILABLE_DOMAINS,
        help="Single domain to query.",
    )
    ld_p.add_argument(
        "--domains",
        help=(
            "Batch query up to 5 domains. Comma-separated or JSON array.\n"
            f"Available: {', '.join(AVAILABLE_DOMAINS)}\n"
            "Takes precedence over --domain."
        ),
    )
    ld_p.set_defaults(func=cmd_list_domains)

    ext_p = subparsers.add_parser(
        "extract",
        help="Fetch full page content from a URL",
        description=(
            "Extract the full content of a web page and return it as Markdown.\n\n"
            "Use this when search snippets are insufficient, you need to verify\n"
            "data, or want to extract structured content (tables, code, etc.).\n\n"
            "Note: Output is truncated at 50,000 characters. Only HTML pages\n"
            "are supported (not PDFs, images, etc.)."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    ext_p.add_argument("url", nargs="?", help="Target URL to extract content from (http(s)://).")
    ext_p.add_argument("--url", "-u", dest="url_opt", help="Target URL to extract content from (alternative to positional arg).")
    ext_p.set_defaults(func=cmd_extract)

    batch_p = subparsers.add_parser(
        "batch_search",
        help="Execute 2-5 search queries in parallel",
        description=(
            "Run multiple independent search queries in a single API call.\n"
            "Each query follows the same parameter structure as the 'search' command.\n"
            "A single query failure does not block others; results are merged.\n\n"
            "Queries are provided as a JSON array of objects. Each object supports\n"
            "the same fields as 'search': query, domain, sub_domain, content_types,\n"
            "zone, max_results, freshness."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "examples:\n"
            '  anysearch batch_search --query AAPL --query GOOG\n'
            '  anysearch batch_search --queries \'[{\"query\":\"AAPL\"},{\"query\":\"GOOG\"}]\'\n'
            '  anysearch batch_search \'[{\"query\":\"AAPL\"},{\"query\":\"GOOG\"}]\'\n'
            '  anysearch batch_search --queries @queries.json\n'
        ),
    )
    batch_p.add_argument(
        "queries",
        nargs="?",
        help=(
            'JSON array of search query objects (1-5 items). '
            'Tolerates PowerShell quote-stripping automatically.\n'
            'Each object supports: query (required), domain, sub_domain, content_types, zone, max_results, freshness.\n'
            'Example: \'[{"query":"AAPL"},{"query":"GOOG"}]\''
        ),
    )
    batch_p.add_argument(
        "--queries", "-q", dest="queries_opt",
        help="JSON array of search query objects (alternative to positional arg). Prefix @ to read from file.",
    )
    batch_p.add_argument(
        "--query",
        action="append",
        dest="query_items",
        help="Shorthand: repeatable single-query string. Easier for PowerShell. Up to 5.",
    )
    batch_p.set_defaults(func=cmd_batch_search)

    doc_p = subparsers.add_parser(
        "doc",
        help="Print AI-facing interface specification",
    )
    doc_p.set_defaults(func=cmd_doc)

    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    if args.command is None:
        parser.print_help()
        sys.exit(0)
    args.func(args)


if __name__ == "__main__":
    main()

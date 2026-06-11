#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");
const https = require("https");

process.stdout.setDefaultEncoding && process.stdout.setDefaultEncoding("utf-8");

const ENDPOINT = "https://api.anysearch.com/mcp";

const AVAILABLE_DOMAINS = [
  "code","tech","fashion","travel","home","ecommerce",
  "gaming","film","music","finance","academic","legal",
  "business","ip","security","education","health","religion",
  "geo","environment","energy","ugc",
];

const CONTENT_TYPES = [
  "web","news","code","doc","academic","data","image","video","audio",
];

const FRESHNESS_VALUES = ["day","week","month","year"];
const ZONES = ["cn","intl"];

function loadEnv() {
  const envPaths = [path.join(__dirname, ".env"), path.join(__dirname, "..", ".env")];
  for (const envPath of envPaths) {
    if (fs.existsSync(envPath)) {
      const lines = fs.readFileSync(envPath, "utf-8").split(/\r?\n/);
      for (const raw of lines) {
        const line = raw.replace(/#.*$/, "").trim();
        if (!line || line.indexOf("=") === -1) continue;
        const idx = line.indexOf("=");
        const key = line.substring(0, idx).trim();
        let val = line.substring(idx + 1).trim().replace(/^["']|["']$/g, "");
        if (!(key in process.env)) process.env[key] = val;
      }
    }
  }
}

loadEnv();

function httpRequest(url, payload, apikey) {
  const body = JSON.stringify(payload);
  const urlObj = new URL(url);
  const options = {
    hostname: urlObj.hostname,
    path: urlObj.pathname,
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Content-Length": Buffer.byteLength(body),
    },
  };
  if (apikey) {
    options.headers["Authorization"] = `Bearer ${apikey}`;
  }

  return new Promise((resolve, reject) => {
    const req = https.request(options, (res) => {
      let data = "";
      res.on("data", (chunk) => (data += chunk));
      res.on("end", () => {
        try {
          const json = JSON.parse(data);
          if (res.statusCode >= 400) {
            reject(new Error(`HTTP ${res.statusCode}: ${JSON.stringify(json)}`));
            return;
          }
          if (json.error) {
            reject(new Error(json.error.message || JSON.stringify(json.error)));
            return;
          }
          const content = json.result && json.result.content;
          if (Array.isArray(content)) {
            const textItem = content.find((c) => c.type === "text");
            if (textItem) {
              resolve(textItem.text);
              return;
            }
          }
          resolve(JSON.stringify(json.result || json, null, 2));
        } catch (e) {
          reject(new Error(`Invalid JSON response: ${data.slice(0, 500)}`));
        }
      });
    });
    req.setTimeout(30000, () => {
      req.destroy();
      reject(new Error("Timeout: The API request timed out."));
    });
    req.on("error", (e) => reject(new Error(`Connection Error: ${e.message}`)));
    req.write(body);
    req.end();
  });
}

async function callApi(toolName, args, apikey) {
  const payload = {
    jsonrpc: "2.0",
    id: 1,
    method: "tools/call",
    params: { name: toolName, arguments: args },
  };
  try {
    return await httpRequest(ENDPOINT, payload, apikey);
  } catch (e) {
    console.error(e.message);
    process.exit(1);
  }
}

function parseJsonList(value) {
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : [parsed];
  } catch (_) {
    return value.split(",").map((s) => s.trim()).filter(Boolean);
  }
}

async function cmdSearch(opts) {
  const args = { query: opts.query };

  if (opts.domain) {
    args.domain = opts.domain;
    if (opts.subDomain) args.sub_domain = opts.subDomain;
    if (opts.subDomainParams) {
      try {
        args.sub_domain_params = JSON.parse(opts.subDomainParams);
      } catch (_) {
        console.error("Error: --sub_domain_params must be valid JSON");
        process.exit(1);
      }
    }
  }

  if (opts.contentTypes) args.content_types = parseJsonList(opts.contentTypes);
  if (opts.zone) args.zone = opts.zone;
  if (opts.maxResults !== undefined) args.max_results = opts.maxResults;
  if (opts.freshness) args.freshness = opts.freshness;

  const result = await callApi("search", args, opts.apiKey);
  console.log(result);
}

async function cmdListDomains(opts) {
  let args;
  if (opts.domains) {
    args = { domains: parseJsonList(opts.domains) };
  } else if (opts.domain) {
    args = { domain: opts.domain };
  } else {
    console.error("Error: provide --domain or --domains");
    process.exit(1);
  }

  const result = await callApi("list_domains", args, opts.apiKey);
  console.log(result);
}

async function cmdExtract(opts) {
  const url = opts.url;
  if (!url) {
    console.error("Error: url is required");
    process.exit(1);
  }
  const result = await callApi("extract", { url }, opts.apiKey);
  console.log(result);
}

function repairJson(raw) {
  raw = raw.trim();
  if (raw.startsWith("{") && !raw.startsWith("[")) raw = "[" + raw + "]";
  if (raw.startsWith("[")) {
    const content = raw.slice(1, -1).trim();
    if (!content) return [];
    const items = splitJsonItems(content);
    return items.map((item) => {
      item = item.trim().replace(/^,|,$/g, "");
      if (!item) return null;
      if (item.startsWith("{")) return repairJsonObject(item);
      return { query: item.trim().replace(/^['"]|['"]$/g, "") };
    }).filter(Boolean);
  }
  return [{ query: raw.trim().replace(/^['"]|['"]$/g, "") }];
}

function splitJsonItems(s) {
  let depth = 0;
  let current = "";
  const items = [];
  for (const ch of s) {
    if (ch === "{") depth++;
    else if (ch === "}") depth--;
    if (ch === "," && depth === 0) {
      items.push(current);
      current = "";
    } else {
      current += ch;
    }
  }
  if (current.trim()) items.push(current);
  return items;
}

function repairJsonObject(s) {
  const inner = s.trim().replace(/^{|}$/g, "").trim();
  if (!inner) return {};
  const pairs = splitJsonItems(inner);
  const result = {};
  for (const pair of pairs) {
    const p = pair.trim().replace(/^,|,$/g, "");
    if (!p || p.indexOf(":") === -1) continue;
    const colon = p.indexOf(":");
    const key = p.substring(0, colon).trim().replace(/^['"]|['"]$/g, "");
    let val = p.substring(colon + 1).trim();
    if (val.startsWith("{")) {
      try { result[key] = JSON.parse(val); } catch (_) { result[key] = repairJsonObject(val); }
    } else if (val.startsWith("[")) {
      try { result[key] = JSON.parse(val); } catch (_) { result[key] = val.slice(1, -1).split(","); }
    } else if (val === "true") {
      result[key] = true;
    } else if (val === "false") {
      result[key] = false;
    } else if (val === "null") {
      result[key] = null;
    } else {
      try { result[key] = JSON.parse(val); } catch (_) { result[key] = val.replace(/^['"]|['"]$/g, ""); }
    }
  }
  return result;
}

async function cmdBatchSearch(opts) {
  let queries;

  if (opts.queryItems && opts.queryItems.length > 0) {
    if (opts.queryItems.length > 5) {
      console.error("Error: batch_search supports a maximum of 5 queries");
      process.exit(1);
    }
    queries = opts.queryItems.map((q) => ({ query: q }));
  } else if (opts.queries) {
    let raw = opts.queries;
    if (raw.startsWith("@")) {
      const fpath = raw.substring(1);
      if (!fs.existsSync(fpath)) {
        console.error(`Error: file not found: ${fpath}`);
        process.exit(1);
      }
      raw = fs.readFileSync(fpath, "utf-8");
    }
    try {
      const parsed = JSON.parse(raw);
      queries = Array.isArray(parsed) ? parsed : [parsed];
    } catch (_) {
      queries = repairJson(raw);
    }
  } else {
    console.error("Error: provide --queries or --query");
    process.exit(1);
  }

  if (queries.length < 1) {
    console.error("Error: queries must contain at least 1 item");
    process.exit(1);
  }
  if (queries.length > 5) {
    console.error("Error: batch_search supports a maximum of 5 queries");
    process.exit(1);
  }

  const result = await callApi("batch_search", { queries }, opts.apiKey);
  console.log(result);
}

function cmdDoc() {
  console.log(`# AnySearch Interface Specification (for AI Agent)

## Protocol
- Endpoint: POST https://api.anysearch.com/mcp
- Format: JSON-RPC 2.0, method = "tools/call"
- Auth: Header "Authorization: Bearer <API_KEY>" (optional, anonymous has lower rate limits)

## CLI Invocation (Node.js)

\`\`\`
node <skill_dir>/scripts/anysearch_cli.js <command> [options]
\`\`\`

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

\`\`\`
User query
  |
  +-- Has structured identifiers? (Stock:/CVE:/DOI:/IATA:/patent etc.)
  |     YES -> 1) node scripts/anysearch_cli.js list_domains --domain X
  |             2) read query_format from result -> construct query accordingly
  |             3) node scripts/anysearch_cli.js search "<query>" --domain X --sub_domain Y --zone cn
  |
  +-- Multiple independent intents?
  |     YES -> node scripts/anysearch_cli.js batch_search --query "..." --query "..."
  |
  +-- Need deeper content than snippets?
        YES -> node scripts/anysearch_cli.js extract "https://example.com/article"

  Otherwise -> node scripts/anysearch_cli.js search "<general query>"
\`\`\`

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

\`\`\`bash
node scripts/anysearch_cli.js search "What is the capital of France"
\`\`\`

\`\`\`bash
node scripts/anysearch_cli.js search "quantum computing breakthroughs 2025" --max_results 5 --freshness month
\`\`\`

### Scenario 2: Search with content type filter — find video or image results

\`\`\`bash
node scripts/anysearch_cli.js search "how to bake sourdough bread" --content_types video --max_results 3
\`\`\`

\`\`\`bash
node scripts/anysearch_cli.js search "Mount Everest" --content_types image --max_results 5
\`\`\`

### Scenario 3: Vertical search — stock market data (structured identifier)

Step 1: Discover available sub_domains for finance:

\`\`\`bash
node scripts/anysearch_cli.js list_domains --domain finance
\`\`\`

Step 2: Search with the correct sub_domain and query format:

\`\`\`bash
node scripts/anysearch_cli.js search "AAPL" --domain finance --sub_domain finance.us_stock --zone cn --max_results 5
\`\`\`

### Scenario 4: Vertical search — academic paper lookup

\`\`\`bash
node scripts/anysearch_cli.js list_domains --domain academic
\`\`\`

\`\`\`bash
node scripts/anysearch_cli.js search "10.1038/s41586-020-2649-2" --domain academic --sub_domain academic.doi --max_results 3
\`\`\`

### Scenario 5: Vertical search — security vulnerability (CVE)

\`\`\`bash
node scripts/anysearch_cli.js list_domains --domain security
\`\`\`

\`\`\`bash
node scripts/anysearch_cli.js search "CVE-2024-3094" --domain security --sub_domain security.cve --max_results 3
\`\`\`

### Scenario 6: Batch search — multiple independent queries in one call

\`\`\`bash
node scripts/anysearch_cli.js batch_search --query "AAPL stock price" --query "TSLA earnings 2025" --query "GOOG market cap"
\`\`\`

With full query objects:

\`\`\`bash
node scripts/anysearch_cli.js batch_search --queries '[{"query":"AAPL","domain":"finance","sub_domain":"finance.us_stock"},{"query":"python async http","domain":"code","sub_domain":"code.general"}]'
\`\`\`

### Scenario 7: Extract full page content

\`\`\`bash
node scripts/anysearch_cli.js extract "https://en.wikipedia.org/wiki/Quantum_computing"
\`\`\`

### Scenario 8: News search with time filter

\`\`\`bash
node scripts/anysearch_cli.js search "AI regulation" --content_types news --freshness day --max_results 5
\`\`\`

### Scenario 9: Search with API key

\`\`\`bash
node scripts/anysearch_cli.js search "climate change policy 2025" --api_key <your_api_key> --max_results 3
\`\`\`

### Scenario 10: China-specific vertical search (requires zone=cn)

\`\`\`bash
node scripts/anysearch_cli.js search "600519" --domain finance --sub_domain finance.cn_stock --zone cn --max_results 5
\`\`\`

---

## Rate Limit Handling
- On rate limit error with auto_registered api_key in response: present key to user for approval, then save to .env and retry
- On anonymous quota exhausted: inform user that a key provides higher limits; suggest configuring one via .env or environment variable`);
}

function usage() {
  console.log(`AnySearch CLI - Unified real-time search client.

Usage: anysearch.js <command> [options]

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
  anysearch.js search "quantum computing"
  anysearch.js search "AAPL" --domain finance --sub_domain finance.us_stock
  anysearch.js list_domains --domain finance
  anysearch.js extract https://example.com
  anysearch.js batch_search --query AAPL --query GOOG
  anysearch.js batch_search --queries '[{"query":"AAPL"},{"query":"GOOG"}]'`);
}

function parseArgs(argv) {
  const args = argv.slice(2);
  const command = args[0] || "";
  const rest = args.slice(1);
  const opts = { apiKey: process.env.ANYSEARCH_API_KEY || "" };

  function shiftVal() {
    if (rest.length === 0) {
      console.error(`Error: missing value for ${rest[0] || "option"}`);
      process.exit(1);
    }
    return rest.shift();
  }

  function nextFlag() {
    return rest.length > 0 && rest[0].startsWith("--");
  }

  switch (command) {
    case "search": {
      opts.query = "";
      while (rest.length > 0 && !rest[0].startsWith("-")) {
        opts.query += (opts.query ? " " : "") + rest.shift();
      }
      if (!opts.query && rest.length > 0 && !rest[0].startsWith("-")) {
        opts.query = rest.shift();
      }
      while (rest.length > 0) {
        const flag = rest.shift();
        switch (flag) {
          case "--domain": case "-d": opts.domain = shiftVal(); break;
          case "--sub_domain": case "-s": opts.subDomain = shiftVal(); break;
          case "--sub_domain_params": opts.subDomainParams = shiftVal(); break;
          case "--content_types": case "-t": opts.contentTypes = shiftVal(); break;
          case "--zone": case "-z": opts.zone = shiftVal(); break;
          case "--max_results": case "-m": opts.maxResults = parseInt(shiftVal(), 10); break;
          case "--freshness": case "-f": opts.freshness = shiftVal(); break;
          case "--api_key": opts.apiKey = shiftVal(); break;
          default: console.error(`Unknown flag: ${flag}`); usage(); process.exit(1);
        }
      }
      if (!opts.query) {
        console.error("Error: query is required");
        process.exit(1);
      }
      return { action: "search", opts };
    }

    case "list_domains": {
      while (rest.length > 0) {
        const flag = rest.shift();
        switch (flag) {
          case "--domain": opts.domain = shiftVal(); break;
          case "--domains": opts.domains = shiftVal(); break;
          case "--api_key": opts.apiKey = shiftVal(); break;
          default: console.error(`Unknown flag: ${flag}`); process.exit(1);
        }
      }
      return { action: "listDomains", opts };
    }

    case "extract": {
      opts.url = "";
      while (rest.length > 0 && !rest[0].startsWith("-")) {
        opts.url += (opts.url ? " " : "") + rest.shift();
      }
      while (rest.length > 0) {
        const flag = rest.shift();
        switch (flag) {
          case "--url": case "-u": opts.url = shiftVal(); break;
          case "--api_key": opts.apiKey = shiftVal(); break;
          default: console.error(`Unknown flag: ${flag}`); process.exit(1);
        }
      }
      return { action: "extract", opts };
    }

    case "batch_search": {
      opts.queryItems = [];
      opts.queries = undefined;
      let positional = undefined;
      while (rest.length > 0) {
        const flag = rest.shift();
        switch (flag) {
          case "--queries": case "-q": opts.queries = shiftVal(); break;
          case "--query": opts.queryItems.push(shiftVal()); break;
          case "--api_key": opts.apiKey = shiftVal(); break;
          default:
            if (!positional) positional = flag;
            else { console.error(`Unknown argument: ${flag}`); process.exit(1); }
        }
      }
      if (positional) opts.queries = opts.queries || positional;
      return { action: "batchSearch", opts };
    }

    case "doc":
      return { action: "doc", opts };

    case "-h": case "--help": case "help":
      usage();
      process.exit(0);

    default:
      if (!command) { usage(); process.exit(0); }
      console.error(`Unknown command: ${command}`);
      usage();
      process.exit(1);
  }
}

async function main() {
  const { action, opts } = parseArgs(process.argv);

  switch (action) {
    case "search": await cmdSearch(opts); break;
    case "listDomains": await cmdListDomains(opts); break;
    case "extract": await cmdExtract(opts); break;
    case "batchSearch": await cmdBatchSearch(opts); break;
    case "doc": cmdDoc(); break;
  }
}

main().catch((e) => {
  console.error(e.message);
  process.exit(1);
});

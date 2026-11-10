<div align="center">

<!--
  Asset notes:
  - Badges (license / go / web / website): self-contained SVGs bundled in the repo at docs/images/badges/ — no external network dependency.
  - Logo: docs/images/logo.png (original source: docs/images/logo.svg).
-->
<img src="./docs/images/logo.png" width="120" height="120" alt="Lework" />

# Lework

Open-source, self-hostable, enterprise-grade AI work platform.

> **Current stage serves individual users only: a single-user, multi-node OpenCode control plane.**

[![License](./docs/images/badges/license.svg)](./LICENSE)
![Go](./docs/images/badges/go.svg)
![Web](./docs/images/badges/web.svg)
[![Website](./docs/images/badges/website.svg)](https://lework.ai/)

**Contents** · [Current Stage](#current-stage) · [Enterprise Vision](#enterprise-vision) · [Understand in a Minute](#understand-in-a-minute) · [Core Capabilities](#core-capabilities) · [System Architecture](#system-architecture) · [Quick Start](#quick-start) · [Development Guide](#development-guide) · [Related Documents](#related-documents) · [Contributing](#contributing) · [Community & Support](#community--support) · [License](#license)

</div>

---

## Current Stage

Manage `lework agent` on multiple machines from a modern GUI; each agent manages its local OpenCode:

- One view for nodes, project directories, sessions, tool calls, and permission approvals;
- Inspect diffs, browse files, and open a session-level terminal;
- Agents connect outbound to the control plane; no inbound ports and no SSH;
- Session and execution state come from OpenCode; Lework does not implement its own Agent Runtime.

**Target users**: individual developers and researchers with two or more machines (laptop, workstation, server, DGX Spark).

- Available today: [Current limitations](docs/product/current-limitations.md)
- Roadmap: [I0–I5](docs/product/roadmap.md)
- Positioning: [Product overview](docs/product/overview.md)

## Enterprise Vision

"Enterprise-grade" describes engineering quality, private deployment, and self-hostability. It belongs to the brand and marketing layer and **does not mean those services are available today**.

Organizations, departments, RBAC, multi-tenancy, billing, and a built-in knowledge base are outside the current scope and are not advertised as shipped capabilities. Documents from the old enterprise stage are archived under [docs/archive/](docs/archive/README.md).

## Understand in a Minute

```text
Control plane (browser on your machine)
  ├── Node A: lework agent → opencode serve
  ├── Node B: lework agent → opencode serve
  └── Node C (DGX Spark): lework agent → opencode serve

Pick a node → pick a project directory → open or create a session → send a task
  → watch messages / tools / approvals → inspect diff / files / terminal
```

One window for many machines, without one TUI per machine.

## Core Capabilities

| Capability | Description | Stage |
|---|---|---|
| Multi-node registration & heartbeat | Agents connect outbound over WSS; reconnect; no inbound ports | Shipped |
| OpenCode process management | Agent starts and supervises local `opencode serve`, loopback only | Shipped |
| Unified sessions | Create / resume / interrupt sessions by directory; messages, tools, approvals | Shipped |
| Diff / files / terminal | Per-turn diff; file browse and download; session-level PTY | Shipped |
| Multi-node scheduling | Choose the execution node | I2 |
| Git task context | Project on A, execute on B, return to A | I3 |
| Config & Action control plane | Read, write, roll back plugins, MCP, models, Actions | I4 |

## System Architecture

```text
Browser (Next.js: frontend/apps/control-plane)
    │ HTTP + SSE + WebSocket
    ▼
Lework Control Plane (Go + SQLite, `lework serve`)
    │ WSS outbound
    ▼
lework agent (`lework agent`) ──► opencode serve (127.0.0.1)
```

- Control plane: node registry, event hub, browser API, agent gateway, SQLite storage;
- Node side: `lework agent` handles registration, heartbeat, OpenCode supervision, PTY, and local proxy (Git/config are I3/I4);
- Source of truth: sessions, messages, diffs, approvals, and files all come from OpenCode.

See [Architecture overview](docs/architecture/overview.md) and [As-built baseline](docs/architecture/as-built.md).

## Quick Start

### Prerequisites

- Go 1.25+
- Node.js / pnpm (to build the control plane UI)
- `opencode` installed on each target node

### Build

```bash
git clone https://github.com/insmtx/Lework.git
cd Lework
make build          # builds frontend/apps/control-plane and produces bundles/lework
```

Backend only:

```bash
go build -o ./bundles/lework ./backend/cmd/lework/
```

Without generated frontend assets, `/` shows a build notice; the API still works.

### Start the control plane

```bash
./bundles/lework serve \
  --listen 127.0.0.1:8787 \
  --db ~/.config/Lework/controlplane.db \
  --enrollment-token "$LEWORK_ENROLLMENT_TOKEN"
```

It binds to loopback by default. Open `http://127.0.0.1:8787/`.

### Start a node agent

```yaml
# /etc/lework/agent.yaml
control_plane_url: ws://127.0.0.1:8787/api/agent/connect
enrollment_token: "<same token as the control plane>"
node_name: dev-box
workspace_roots:
  - /home/me/projects
opencode:
  binary: opencode
  port: 4096
  service_mode: shared
```

```bash
./bundles/lework agent --config /etc/lework/agent.yaml
```

`workspace_roots` restricts where sessions can be created and files can be read. Full protocol and troubleshooting: [Control plane operations](docs/operations/control-plane.md).

## Development Guide

### Repository Structure

```text
Lework/
├── backend/
│   ├── cmd/lework/              # serve / agent entrypoints
│   ├── internal/controlplane/   # control plane: nodes, events, browser API, agent gateway, SQLite
│   ├── internal/agentd/         # node agent: registration, OpenCode supervision, PTY, WSS transport
│   ├── agent/ tools/ types/     # legacy enterprise execution core (not used by the control plane)
│   └── config/
├── frontend/
│   ├── apps/control-plane/      # current browser UI
│   └── apps/web, apps/desktop/  # legacy products, unmaintained
├── deployments/
├── docs/                        # product / architecture / design / frontend / operations / archive
├── Makefile / go.mod / AGENTS.md / CONTRIBUTING.md / LICENSE
```

### Common Commands

```bash
make build                       # build control-plane UI + lework binary
make frontend-control-plane      # build the control-plane UI only
go test ./...                    # default tests
go test ./backend/cmd/lework ./backend/internal/agentd/...
go vet ./...
```

### Conventions & Notes

- **Module path**: internal imports use `github.com/insmtx/Lework/...`;
- **No Agent Runtime in the control plane**: do not add a home-grown runtime, task state machine, or org/permission model;
- **OpenCode is the only source of truth**: do not duplicate session / message / event state;
- **No inbound ports on nodes**: every connection is outbound from the agent;
- See [AGENTS.md](AGENTS.md) for the full engineering constraints.

## Related Documents

| Document | Content |
|---|---|
| [docs/product/overview.md](docs/product/overview.md) | Product positioning and core concepts |
| [docs/product/roadmap.md](docs/product/roadmap.md) | I0–I5 roadmap and exit criteria |
| [docs/product/current-limitations.md](docs/product/current-limitations.md) | Current capability boundary (I1-B) |
| [docs/architecture/overview.md](docs/architecture/overview.md) | Target architecture |
| [docs/architecture/as-built.md](docs/architecture/as-built.md) | As-built baseline |
| [docs/architecture/agent-runtime.md](docs/architecture/agent-runtime.md) | lework agent and OpenCode boundary |
| [docs/operations/control-plane.md](docs/operations/control-plane.md) | Install, startup, browser protocol |
| [docs/operations/troubleshooting.md](docs/operations/troubleshooting.md) | Troubleshooting |
| [docs/operations/project-structure.md](docs/operations/project-structure.md) | Repository structure index |
| [docs/README.md](docs/README.md) | Documentation rules and index |
| [docs/frontend/overview.md](docs/frontend/overview.md) | Control-plane frontend architecture |
| [lework.ai](https://lework.ai/) | Lework website |

## Contributing

We welcome all forms of contribution — Issues, documentation, code, and usage feedback.

- **File an Issue**: report bugs or suggest features at [GitHub Issues](https://github.com/insmtx/Lework/issues);
- **Submit a PR**: Fork this repository → create a feature branch → commit your changes → open a Pull Request. Opening an Issue to discuss the design first is recommended;
- **Development conventions**: before you start, read [AGENTS.md](AGENTS.md) at the repo root (repository structure, engineering and code conventions) and the [Development Guide](#development-guide) above.

## Community & Support

- Usage questions / bug reports: [GitHub Issues](https://github.com/insmtx/Lework/issues)
- Feature requests & discussions: same place, please tag them with `feature` / `discussion`
- Official website: [lework.ai](https://lework.ai/)

## License

Lework is licensed under the **[Lework Open Source License](./LICENSE)**: based on Apache License 2.0, with additional commercial-use conditions (the English text is legally binding). For specific cases such as multi-tenant SaaS, the LOGO and copyright information, contact the maintainer for a commercial license — see the LICENSE file for details.

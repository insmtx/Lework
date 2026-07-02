# Lework

## Enterprise Digital Workforce Operating System

> Build, orchestrate and govern AI-powered digital assistants for enterprise.

---

## 🚀 What is Lework?

**Lework** is an enterprise-grade Multi-Agent Operating System designed to power the next generation of digital workforce.

It is not a chatbot framework.
It is not a simple workflow engine.

Lework is:

> A distributed, governance-first AI execution system for enterprise digital transformation.

Lework enables organizations to:

* Design AI-powered digital assistants
* Orchestrate multi-agent workflows
* Govern skills, models, and permissions
* Run intelligent task execution pipelines
* Operate in both private enterprise environments and SaaS sandbox mode

---

## 🧠 Why Lework?

Traditional workflow systems focus on deterministic task automation.

Modern enterprises require:

* Intelligent decision-making
* Cross-system reasoning
* Multi-agent collaboration
* Cost-aware model routing
* Auditable AI execution
* Enterprise-grade governance

Lework is built to meet these needs.

Compared to traditional workflow engines such as DeerFlow:

* Lework embeds cognitive agents into workflows
* Lework includes model routing and cost governance
* Lework supports multi-tenant enterprise deployment
* Lework is designed as an AI OS, not just a flow engine

---

## 🎯 Design Principles

Lework enforces strict architectural invariants to ensure governance and reliability:

1. **Agent never directly calls external systems** - All external interactions go through Tools
2. **Skill never performs orchestration logic** - Skills compose Tools, not workflows
3. **Control plane never executes runtime logic** - Clear separation of concerns
4. **All workflow execution must be persisted** - Replayable and auditable
5. **All model usage must be measurable** - Cost-aware and governable

For detailed design philosophy, see [Design Philosophy](docs/architecture/design-philosophy.md).

---

## 🏢 Target Scenarios

Lework is designed for:

### Enterprise Internal Digital Transformation

* Digital assistants for operations
* Intelligent approval systems
* Automated reporting
* Cross-system workflow automation
* AI-assisted decision engines

### SaaS Sandbox Mode

* Demonstration environments
* Trial accounts
* Limited skill library
* Token quota enforcement
* No sensitive system integration

---

## 🔐 Enterprise-First Capabilities

* Multi-tenant isolation
* RBAC access control
* Audit logs
* Skill-level permission control
* Cost tracking
* SLA-aware execution
* Private deployment support

---

## 🔄 Execution Flow

Lework follows a unified event-driven execution model:

```
User → Event Gateway → EventBus → Control Plane → Orchestrator 
→ Runtime Manager → Agent/Edge Runtime → Skill → Tool → EventBus → Client
```

All execution is:

* **Replayable** - Complete execution history recorded
* **Observable** - Full链路 tracing and monitoring
* **Auditable** - Comprehensive audit logs

For detailed architecture, see [Architecture Documentation](docs/architecture/overview.md).

---

## 🧩 Extensibility

Lework supports plugin-based architecture:

* Skill plugins
* Agent templates
* Model providers
* Memory backends
* Workflow templates

All plugins must be:

* Versioned
* Isolated
* Auditable

---

## 🛣 Roadmap

### Phase 1 – Core Execution Layer

* DAG execution engine
* Agent runtime
* Model router
* Multi-tenant basics

### Phase 2 – Enterprise Intelligence

* Cross-agent collaboration
* Cost optimization engine
* Distributed scheduler
* Observability suite

### Phase 3 – AI OS Evolution

* Agent federation
* Autonomous optimization
* Workflow marketplace
* Digital workforce marketplace

---

## ⚠ Non-Goals

Lework is NOT:

* A prompt playground
* A simple chatbot UI
* A research-only autonomous agent simulator
* A decentralized AI experiment

---

## 🧬 Philosophy

Lework treats AI agents as:

> First-class digital assistants with governance, accountability, and operational boundaries.

We believe the future enterprise stack will include:

* Human employees
* Software systems
* Digital assistants (AI Agents)

Lework is designed to operate the third category.

---

## 📚 Documentation

Complete documentation is available in the `docs/` directory:

| Document | Description |
|----------|-------------|
| [architecture/overview.md](docs/architecture/overview.md) | AI OS architecture design |
| [architecture/design-philosophy.md](docs/architecture/design-philosophy.md) | Core design philosophy and principles |
| [architecture/agent-runtime.md](docs/architecture/agent-runtime.md) | Agent Runtime architecture |
| [architecture/workspace-artifact.md](docs/architecture/workspace-artifact.md) | Agent workspace and artifact design |
| [architecture/system-design.md](docs/architecture/system-design.md) | System architecture design |
| [product/prd.md](docs/product/prd.md) | Product requirements — AI Workspace (v3) |
| [product/planning.md](docs/product/planning.md) | Roadmap — business domains |
| [design/tech-design.md](docs/design/tech-design.md) | Technical design — skill schema, rendering engine |
| [design/git-storage.md](docs/design/git-storage.md) | Git-based file storage design |
| [design/presigned-url.md](docs/design/presigned-url.md) | Presigned URL architecture |
| [operations/troubleshooting.md](docs/operations/troubleshooting.md) | Common issues and solutions |

---

## 🤝 Contributing

We welcome skill plugins, model adapters, workflow templates, observability integrations, and security enhancements. See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

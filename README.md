# Leros

## Enterprise Digital Workforce Operating System

> Build, orchestrate and govern AI-powered digital assistants for enterprise.

---

## 🚀 What is Leros?

**Leros** is an enterprise-grade Multi-Agent Operating System designed to power the next generation of digital workforce.

It is not a chatbot framework.
It is not a simple workflow engine.

Leros is:

> A distributed, governance-first AI execution system for enterprise digital transformation.

Leros enables organizations to:

* Design AI-powered digital assistants
* Orchestrate multi-agent workflows
* Govern skills, models, and permissions
* Run intelligent task execution pipelines
* Operate in both private enterprise environments and SaaS sandbox mode

---

## 🧠 Why Leros?

Traditional workflow systems focus on deterministic task automation.

Modern enterprises require:

* Intelligent decision-making
* Cross-system reasoning
* Multi-agent collaboration
* Cost-aware model routing
* Auditable AI execution
* Enterprise-grade governance

Leros is built to meet these needs.

Compared to traditional workflow engines such as DeerFlow:

* Leros embeds cognitive agents into workflows
* Leros includes model routing and cost governance
* Leros supports multi-tenant enterprise deployment
* Leros is designed as an AI OS, not just a flow engine

---

## 🎯 Design Principles

Leros enforces strict architectural invariants to ensure governance and reliability:

1. **Agent never directly calls external systems** - All external interactions go through Tools
2. **Skill never performs orchestration logic** - Skills compose Tools, not workflows
3. **Control plane never executes runtime logic** - Clear separation of concerns
4. **All workflow execution must be persisted** - Replayable and auditable
5. **All model usage must be measurable** - Cost-aware and governable

For detailed design philosophy, see [Design Philosophy](docs/DESIGN_PHILOSOPHY.md).

---

## 🏢 Target Scenarios

Leros is designed for:

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

Leros follows a unified event-driven execution model:

```
User → Event Gateway → EventBus → Control Plane → Orchestrator 
→ Runtime Manager → Agent/Edge Runtime → Skill → Tool → EventBus → Client
```

All execution is:

* **Replayable** - Complete execution history recorded
* **Observable** - Full链路 tracing and monitoring
* **Auditable** - Comprehensive audit logs

For detailed architecture, see [Architecture Documentation](docs/ARCHITECTURE.md).

---

## 🧩 Extensibility

Leros supports plugin-based architecture:

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

Leros is NOT:

* A prompt playground
* A simple chatbot UI
* A research-only autonomous agent simulator
* A decentralized AI experiment

---

## 🧬 Philosophy

Leros treats AI agents as:

> First-class digital assistants with governance, accountability, and operational boundaries.

We believe the future enterprise stack will include:

* Human employees
* Software systems
* Digital assistants (AI Agents)

Leros is designed to operate the third category.

---

## 📜 License

(To be determined — Apache 2.0 / Commercial Hybrid / Enterprise License)

---

---

## 📚 Documentation

Complete documentation is available in the `docs/` directory:

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | AI OS architecture design (v2 - Three-Plane Model) |
| [DESIGN_PHILOSOPHY.md](docs/DESIGN_PHILOSOPHY.md) | Core design philosophy and principles |
| [PRD.md](docs/PRD.md) | 产品需求文档 — AI 工作协作系统 |
| [SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md) | 系统架构设计 — 实体关系、平台引擎、外部连接 |
| [TECH_DESIGN.md](docs/TECH_DESIGN.md) | 技术设计 — 技能 Schema、渲染引擎、模板规格 |
| [PLANNING.md](docs/PLANNING.md) | 规划项 — 业务板块路线图（文档撰写/研发/AIOps） |
| [GITHUB_AUTH_SETUP.md](docs/GITHUB_AUTH_SETUP.md) | GitHub OAuth integration guide |
| [GITHUB_WEBHOOK_TROUBLESHOOTING.md](docs/GITHUB_WEBHOOK_TROUBLESHOOTING.md) | GitHub webhook troubleshooting |
| [PR_EVENT_FLOW.md](docs/PR_EVENT_FLOW.md) | GitHub PR event processing verification |
| [TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | Common issues and solutions |

---

## 🤝 Contributing

We welcome:

* Skill plugins
* Model adapters
* Workflow templates
* Observability integrations
* Security enhancements

Enterprise partners are welcome to collaborate.


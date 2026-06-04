# Issue 分类与标签体系

本文档定义 Leros/SingerOS 仓库的 Issue 分类和标签命名规则。Issue Forms 默认只添加类型标签和 `status/needs-triage`，领域、优先级和后续状态由维护者在 triage 时补充。

## 分类

| 分类 | 模板 | 默认标签 | 适用场景 |
|------|------|----------|----------|
| 缺陷报告 | `bug_report.yml` | `type/bug`, `status/needs-triage` | 可复现的异常行为、错误或回归问题 |
| 功能建议 | `feature_request.yml` | `type/feature`, `status/needs-triage` | 新能力、产品改进或体验优化 |
| 开发任务 | `task.yml` | `type/task`, `status/needs-triage` | 边界明确、可交付、可验收的工程任务 |
| 文档问题 | `docs.yml` | `type/docs`, `status/needs-triage` | 文档缺失、错误、过期或表达不清 |
| 使用问题 | `question.yml` | `type/question`, `status/needs-triage` | 使用方式、设计取舍、集成方式或澄清问题 |
| 安全问题 | `security.yml` | `type/security`, `status/needs-triage` | 漏洞、密钥泄露、权限绕过等安全相关事项 |

安全相关问题不应在公开 Issue 中粘贴漏洞细节、token、密钥、内部地址或可直接利用的复现材料。维护者 triage 后再决定是否转入私下处理流程。

## 标签体系

### 类型标签

| 标签 | 说明 |
|------|------|
| `type/bug` | 缺陷或异常行为 |
| `type/feature` | 新功能建议 |
| `type/task` | 明确的工程任务 |
| `type/docs` | 文档补充或修正 |
| `type/question` | 使用问题、讨论、澄清 |
| `type/security` | 安全相关问题 |

### 状态标签

| 标签 | 说明 |
|------|------|
| `status/needs-triage` | 待维护者分流 |
| `status/accepted` | 已接受，准备排期或实现 |
| `status/blocked` | 被依赖、外部条件或设计决策阻塞 |
| `status/in-progress` | 正在处理中 |

### 领域标签

| 标签 | 说明 |
|------|------|
| `area/backend` | 后端服务、API、业务逻辑 |
| `area/frontend` | 前端应用和用户界面 |
| `area/docs` | 文档、示例、贡献指南 |
| `area/infra` | 部署、构建、CI、运行环境 |

### 优先级标签

| 标签 | 说明 |
|------|------|
| `P0` | 阻断主流程、生产不可用、严重安全风险 |
| `P1` | 重要能力不可用或影响核心场景 |
| `P2` | 常规问题或中等优先级改进 |
| `P3` | 低优先级优化、清理、后续增强 |

## Triage 规则

1. 每个 Issue 保留一个 `type/` 标签。
2. 每个 Issue 至少保留一个 `status/` 标签，状态变化时移除旧状态。
3. 领域标签由维护者根据实际归属添加，可以有多个。
4. 优先级标签由维护者根据影响范围、紧急程度和路线图判断添加。
5. 避免创建同义标签；新增标签前先检查本文件是否已有对应概念。

---
description: Registry of bid-generator workflows
---

# Workflow Registry

Registry for workflows referenced by `SKILL.md` and [`routing.md`](./routing.md).

**Hard rule**: When adding a workflow, update this registry and [`routing.md`](./routing.md) in the same change.

---

## 1. Main Pipeline Workflows

| ID | Path | Trigger | Preconditions | Output Contract | Blocking Points |
|----|------|---------|---------------|-----------------|-----------------|
| quick-reference | [`quick-reference.md`](./quick-reference.md) | Step 1: 招标文件提供 | 招标文件可读 | 速查表 A~F 六部分 | ⛔ 速查表需用户确认 |
| business-technical-split | [`business-technical-split.md`](./business-technical-split.md) | Step 2: 速查表已确认 | Step 1 完成 | 点对点应答表 + 排除清单 | 无 |
| template-extraction | [`template-extraction.md`](./template-extraction.md) | Step 3: Step 1+2 完成 | 招标文件中存在投标文件格式章节 | 标书底版 docx + 分项清单 | ⛔ 底版和分项需用户确认 |
| service-proposal-writing | [`service-proposal-writing.md`](./service-proposal-writing.md) | Step 4: 底版已确认 | 底版 docx 可用 | 底版中自拟模块已填充 | ⛔ 每模块需用户提供材料 |
| table-backfill | [`table-backfill.md`](./table-backfill.md) | Step 5: Step 1-4 完成 | 应答表 + 方案正文 + 底版偏差表模板 | 填空型模块 + 偏差表 + 报价表已填写 | ⛔ 价格需用户确认 |

## 2. Supporting Workflows

| ID | Path | Trigger | Preconditions | Output Contract |
|----|------|---------|---------------|-----------------|
| routing | [`routing.md`](./routing.md) | 每次请求开始 | — | 路由决策 |
| failure-recovery | [`failure-recovery.md`](./failure-recovery.md) | 任何步骤失败 | 失败发生 | 恢复路径或停止 |
| final-verification | `SKILL.md` Final Verification | Step 5 完成 | 最终 docx 已生成 | 核查结果 |

## 3. Update Checklist

When adding or changing a workflow:

1. Add/update the row in §1 or §2.
2. Update route selection in [`routing.md`](./routing.md).
3. Add a short pointer in `SKILL.md` if the workflow is part of the main pipeline.
4. Keep detailed commands and recovery behavior in the workflow file, not in this registry.

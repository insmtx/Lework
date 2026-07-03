---
description: 路由选择规则 — bid-generator 请求的确定性路由
---

# Routing Rules

路由选择权威。如果本文件与 SKILL.md 在路由选择上有冲突，以本文件为准。

**硬规则**: 当请求匹配已定义路由时，直接进入路由，不要求用户选择。当缺前置条件时，声明缺失后停止，不凭空发明替代路线。

**Registry**: Use [`index.md`](index.md) for the complete workflow list, triggers, preconditions, and outputs.

**Failure recovery**: Use [`failure-recovery.md`](failure-recovery.md) when any step fails.

---

## 1. Main Route Matrix

| Request Shape | Trigger | Route | Preconditions | Output Contract | Stop Condition |
|---------------|---------|-------|---------------|-----------------|----------------|
| 制作标书（有招标文件） | 用户提供 .docx/.pdf 招标文件，"制作标书""写标书""投标" | **完整 5 步流程** | 招标文件存在且可读 | 速查表 → 应答表 → 底版 → 方案 → 偏差报价 → 最终 .docx | 任一阻塞点用户未确认 |
| 只做信息梳理 | "分析招标文件""梳理招标要求" | 仅 Step 1: quick-reference | 招标文件存在 | 速查表（A~F 六部分） | — |
| 已有速查表，只做应答 | "拆分条款""逐条应答" | 仅 Step 2: business-technical-split | 速查表已确认 | 点对点应答表 | — |
| 已有速查表+应答表，只提底版 | "提底版""提取标书格式" | 仅 Step 3: template-extraction | Step 1+2 输出已存在 | 标书底版 docx | — |
| 底版就绪，只写方案 | "写方案""技术方案" | 仅 Step 4: service-proposal-writing | 底版已确认 | 底版中自拟模块已填充 | 用户未提供材料 |
| 方案完成，只回填 | "补表""偏差表""报价表" | 仅 Step 5: table-backfill | Step 1-4 输出已存在 | 偏差表+报价表已填写 | 用户未提供价格 |
| PDF 招标文件 | 招标文件为 .pdf | 先用 pdf-reading skill 提取文本，再走完整流程 | PDF 可读 | 同完整流程 | PDF 无法解析 |

## 2. Fallback Routes

| Scenario | Fallback | Rule |
|----------|----------|------|
| 招标文件中未找到"投标文件格式"章节 | 要求用户确认章节位置 | 不凭空猜测章节名 |
| 分项名称在招标文件中不明确 | 标记"需人工确认"，等待用户输入 | 不自创分项名 |
| 用户缺材料但仍要求继续 | 标记"待补充"后继续 | 不编造信息 |
| 价格未提供 | 偏差表可先填响应情况，报价表留空标记"待补充" | 不编造价格 |

## 3. 禁止的路由行为

| 禁止 | 原因 |
|------|------|
| 让用户在多个路由中选择 | 路由是确定性的，不应让用户选择技术路径 |
| 缺前置条件时凭空发明替代方案 | 必须声明缺失后停止 |
| 在 Step 1 就开始写方案正文 | 必须先确认速查表 |
| 在没有底版的情况下直接生成 docx | 必须先切出底版 |

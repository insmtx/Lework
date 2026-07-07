# Artifact Ownership Specification

> 定义 bid-generator 项目中每个文件的归属关系。谁产出、谁可写、谁只读、如何重建。

## 1. Ownership Matrix

| Artifact | Owner | Role | Read/Write Contract |
|----------|-------|------|-------------------|
| 速查表（chat 输出） | Step 1: quick-reference | 所有后续步骤只读引用 | Step 1 产出完整六部分信息，后续步骤引用但不修改。如果招标文件有澄清补遗，Step 1 可更新 |
| 点对点应答表（chat 输出） | Step 2: business-technical-split | Step 5 回填的直接数据源 | Step 2 生成每条条款的六字段应答（含响应性质）。Step 5 按响应性质分类回填到偏差表，不修改应答内容 |
| 标书底版 docx | Step 3: template-extraction | 从招标文件切出的只读模板 + 后续可写的容器 | Step 3 切出后为纯模板。Step 4 只在自拟模块写入方案正文。Step 5 回填填空型模块、偏差表和报价表。**禁止修改有模板的模块的格式** |
| 方案正文（底版中） | Step 4: service-proposal-writing | 自拟模块内容 | Step 4 直接写入底版中允许投标人自行编写的方案类模块（如服务方案、技术方案、实施方案、服务保障、售后服务、培训方案等，以底版实际标题为准）。Step 5 偏差表可引用（只读），不修改正文 |
| 填空型模块（底版中） | Step 5: table-backfill | 占位符替换 | Step 5 替换封面/投标函/承诺书/基本情况表中的占位符文本。不修改模板格式和固定文案 |
| 偏差表（底版中） | Step 5: table-backfill | 从应答表回填的内容 | Step 5 写入。回填后不得手动修改条款号（与应答表一致） |
| 报价表（底版中） | Step 5: table-backfill | 从方案压缩 + 用户价格 | Step 5 写入。分项名称必须与 Step 1 一致 |
| 最终标书 docx | Step 5 + 最终核查 | 交付物 | 底版 + Step 4 方案 + Step 5 填空模块/偏差报价的组合。最终核查只读 |

## 2. Ownership Invariants

| Invariant | Rule |
|-----------|------|
| **分项名称** | Step 1 固定，Step 2/3/4/5 只读引用，不得改名 |
| **条款号** | Step 2 固定，Step 5 只引用，不得新增/删除/重排 |
| **底版格式** | Step 3 切出后，有模板的模块不得修改格式；自拟模块可写内容 |
| **速查表** | Step 1 产出后，后续步骤不得修改。如有澄清补遗可更新 |
| **报价** | 必须由用户提供，不得编造 |

## 3. Forbidden Actions

| 禁止行为 | 为什么 |
|---------|--------|
| 在 Step 4 修改有模板的模块（投标函、报价表格式、封面格式等） | 模板来自招标文件，改了格式可能废标 |
| 在 Step 4 将方案写成独立 .md 或 .docx 文件 | 方案必须直接写入底版的自拟模块中 |
| 在 Step 3 用 python-docx 新建文档替代 `extract_docx_range.py` 切出的底版 | 必须从招标文件切出底版，100% 保留原格式 |
| 用自己写的 Python 脚本仿造底版提取 | 必须使用 `${SKILL_DIR}/scripts/extract_docx_range.py`，这是唯一合法的底版提取工具 |
| 在 Step 5 中新增或删除偏差表条款号 | 条款号来自 Step 2 的点对点应答表，必须一致 |
| 在 Step 3/4 中重命名分项名称 | 分项名称来自 Step 1，不一致会导致报价废标 |
| 在底版之外单独管理方案内容 | 方案必须写入底版的自拟模块中，不分散 |

## 4. Regeneration Rules

| Derived Artifact | Regenerate From | When |
|-----------------|----------------|------|
| 偏差表内容 | Step 2 应答表 + Step 4 方案 | 方案章节编号变化时需更新引用位置 |
| 报价表服务内容 | Step 4 方案对应分项章节 | 方案内容更新时需同步更新 |
| 最终标书 docx | 底版 + Step 4 方案 + Step 5 偏差报价 | 任何一步修改后需重新生成 |

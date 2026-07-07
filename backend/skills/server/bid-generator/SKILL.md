---
name: bid-generator
description: >
  根据任意招标文件生成完整投标文件。5步流程：速查表→逐条应答→底版提取→方案编写→偏差报价回填。触发词：制作标书、写标书、投标。
---

# Bid Generator Skill — 通用标书生成

**投标文件的结构由招标文件决定，格式由招标模板决定。** 每一步都以前一步的输出为基础。

**Core Pipeline**: `速查表 → 逐条应答 → 提取底版 → 编写方案 → 回填偏差报价 → 最终核查`

> [!CAUTION]
> ## 🚨 Global Execution Discipline

> MUST 1. **先读后做** — 第一步必须通读全文提取速查表。不跳过任何章节。
> MUST 2. **每步读workflow** — 每个 Step 开始前 MUST Read 对应 workflow 文件。不允许根据 SKILL.md 简要描述推测执行。
> MUST 3. **逐条应答，不笼统** — 技术规范写到子条款级，每条独立响应。每条标注响应性质。
> MUST 4. **底版来自招标文件** — 用 `${SKILL_DIR}/scripts/extract_docx_range.py` 从 docx 切出，不新建、不仿造。
> MUST 5. **在底版上填写，不重建** — 有模板的模块填入内容；没有模板的自拟模块写入方案正文。方案直接写入底版 docx，不生成独立文件。不修改模板格式。
> MUST NOT 6. **编造 = 废标** — 绝不编造公司资质、人员、业绩、报价、产品能力。缺失处标记"待补充"。
> MUST NOT 7. **脚本是纯工具** — `${SKILL_DIR}/scripts/extract_docx_range.py` 只接收索引号、执行切分，不含任何业务判断。
> GATE 8. **阻塞点必须硬停** — 标记 ⛔ BLOCKING 的步骤必须等待用户明确回复，不替用户做决定。
> DEFAULT 9. **分项名称全程一致** — Step 1 固定，后续步骤不得改名。

## Rule Strength Labels

| Label | Meaning |
|-------|---------|
| MUST | Required behavior; violation is workflow failure |
| MUST NOT | Forbidden behavior |
| DEFAULT | Used when the user has not specified otherwise |
| GATE | Required checkpoint before entering the next step |
| FALLBACK | Recovery path after the primary path fails |

## Cross-Cutting Authorities

| Concern | Authority | Contract |
|---------|-----------|----------|
| Main pipeline sequencing | This `SKILL.md` | Owns Step 1-5 order, gates, and mandatory commands |
| Route selection | [`workflows/routing.md`](workflows/routing.md) | Owns deterministic route choice |
| Workflow registry | [`workflows/index.md`](workflows/index.md) | Owns workflow trigger/precondition/output inventory |
| Artifact ownership | [`references/artifact-ownership.md`](references/artifact-ownership.md) | Owns which step reads/writes each file |
| Failure recovery | [`workflows/failure-recovery.md`](workflows/failure-recovery.md) | Owns stop/continue decisions for common failures |
| Format standards | [`references/format-standards.md`](references/format-standards.md) | Owns docx generation formatting rules |
| Final checklist | [`references/checklist.md`](references/checklist.md) | Owns post-generation verification |
| Industry knowledge | [`references/industry-knowledge.md`](references/industry-knowledge.md) | Owns domain best practices |

## Main Pipeline Scripts

| Script | Purpose |
|--------|---------|
| `${SKILL_DIR}/scripts/extract_docx_range.py` | 按 body 元素索引范围切出 docx — Step 3 核心工具 |

---

## Role Switching Protocol

🚨 **执行纪律**：每个 Step 开始前，**MUST** 先 Read 对应的 workflow 文件。**不允许根据本 SKILL.md 中的简要描述推测执行。** 未读 workflow 直接执行 = 流程失败。

```
## [Role: 速查表分析]
📖 Read: ${SKILL_DIR}/workflows/quick-reference.md

## [Role: 条款逐条应答]
📖 Read: ${SKILL_DIR}/workflows/business-technical-split.md

## [Role: 底版提取]
📖 Read: ${SKILL_DIR}/workflows/template-extraction.md

## [Role: 方案编写]
📖 Read: ${SKILL_DIR}/workflows/service-proposal-writing.md

## [Role: 偏差报价回填]
📖 Read: ${SKILL_DIR}/workflows/table-backfill.md
```

---

## 📋 Process Overview

| Step | Workflow | Core Task | Gate |
|------|----------|-----------|------|
| 1 | [quick-reference](workflows/quick-reference.md) | 通读全文，提取速查表（A~F 六部分） | ⛔ 速查表确认 |
| 2 | [business-technical-split](workflows/business-technical-split.md) | 逐条提取条款，生成点对点应答表 | 不阻塞 |
| 3 | [template-extraction](workflows/template-extraction.md) | 从招标 docx 切出标书底版 | ⛔ 底版确认 |
| 4 | [service-proposal-writing](workflows/service-proposal-writing.md) | 在自拟模块编写方案正文 | ⛔ 每模块材料 |
| 5 | [table-backfill](workflows/table-backfill.md) | 应答表回填偏差表 + 报价表 + 填空型模块 | ⛔ 价格确认 |
| ✓ | — | 最终核查：逐项核对废标/格式/一致性 | 不阻塞 |

**Route authority**: Use [`workflows/routing.md`](workflows/routing.md) before entering the main pipeline.

**Failure recovery**: Use [`workflows/failure-recovery.md`](workflows/failure-recovery.md) when any step fails.

---

## Step 1: 招标文件速查表

🚧 **GATE**: 用户提供完整招标文件（.docx / .pdf 均可）

📖 Read: [`${SKILL_DIR}/workflows/quick-reference.md`](workflows/quick-reference.md)

**MUST** 通读招标文件全文，提取六部分信息。每条标注来源章节和条款号。

| Section | Content |
|---------|---------|
| A 项目基本信息 | 名称、编号、招标人、限价、服务期、地点 |
| B 关键时间节点 | 截止时间、开标时间、投标有效期 |
| C 硬性门槛 | 资质、业绩、人员、保证金——每一条都是废标红线 |
| D 评分结构 | 商务/技术/价格分值和权重 |
| E 关键商务条件 | 付款、验收、质保、知识产权、违约 |
| F 递交要求 | 电子/纸质、正副本、装订密封 |

**⛔ BLOCKING**: 速查表需用户确认。

```markdown
## ✅ Step 1 Checkpoint
- [x] 速查表 A~F 六部分已填写
- [x] 所有信息标注来源章节和条款号
- [x] 硬性门槛已逐条列出
- [ ] **Next**: Step 2 条款逐条应答（等待用户确认后执行）
```

---

## Step 2: 条款逐条应答

🚧 **GATE**: 速查表已确认

📖 Read: [`${SKILL_DIR}/workflows/business-technical-split.md`](workflows/business-technical-split.md)

遍历招标文件全文，**MUST** 逐条提取需响应的条款。每条生成六字段应答：章节条款号、原文摘要、响应性质（资格证明/商务承诺/技术方案/报价/实质性条款）、我方响应、证明材料位置。

**MUST** 技术规范写到子条款级。**MUST NOT** 笼统写"满足全部技术要求"。

不阻塞。应答表是 Step 5 回填偏差表的直接数据源。

```markdown
## ✅ Step 2 Checkpoint
- [x] 点对点应答表已生成（共 N 条）
- [x] 每条条款已标注响应性质（资格证明/商务承诺/技术方案/报价/实质性条款）
- [x] 技术规范已写到子条款级
- [x] 排除清单已输出
- [ ] **Next**: Step 3 提取标书底版
```

---

## Step 3: 提取标书底版

🚧 **GATE**: Step 1 + 2 完成

📖 Read: [`${SKILL_DIR}/workflows/template-extraction.md`](workflows/template-extraction.md)

AI agent 在招标 docx 的 body 元素中定位"投标文件格式"章节 → 调用 `${SKILL_DIR}/scripts/extract_docx_range.py --from N` 切出底版。**MUST** 100% 保留原格式。

**切出后 MUST 验证**：列出底版前 5 个段落的文本和样式，确认第一页是封面模板而非章节标题或空白页。

**⛔ BLOCKING**: 底版和分项名称需用户确认。

```markdown
## ✅ Step 3 Checkpoint
- [x] 底版 docx 已从招标文件切出（共 N 页）
- [x] 分项名称清单已从报价表提取
- [x] 自拟模块已标注
- [ ] **Next**: Step 4 编写方案正文（等待用户确认底版后开始）
```

---

## Step 4: 编写方案正文

🚧 **GATE**: 底版已确认

📖 Read: [`${SKILL_DIR}/workflows/service-proposal-writing.md`](workflows/service-proposal-writing.md)

**MUST** 写入底版 docx 中允许投标人自行编写的方案类模块（如服务方案、技术方案、实施方案、服务保障、售后服务、培训方案等，以底版实际标题为准）。**MUST NOT** 修改有模板的模块格式，**MUST NOT** 将方案写成独立文件。

按模块向用户索要材料，逐模块编写。写作公式："招标要求→我方理解→建设目标→功能能力→技术实现→交付效果"。

**⛔ BLOCKING**: 每模块需用户提供材料。**DEFAULT** 材料不齐时标记"待补充"后继续。**FALLBACK** 参考 [`workflows/failure-recovery.md`](workflows/failure-recovery.md)。

```markdown
## ✅ Step 4 Checkpoint
- [x] 自拟模块已全部编写（或标记"待补充"）
- [x] 方案内容覆盖所有技术条款响应需求
- [x] 方案章节可被偏差表引用
- [ ] **Next**: Step 5 偏差表和报价回填
```

---

## Step 5: 偏差表和报价回填

🚧 **GATE**: Step 1-4 完成

📖 Read: [`${SKILL_DIR}/workflows/table-backfill.md`](workflows/table-backfill.md)

**第一步：聚合判定。** 将 Step 2 应答表中的条款按招标文件自身章节结构做聚合（商务按主题合并，技术按子条款编号展开，*号以实际标注为准）。聚合后再按响应性质分类入表（资格证明+商务承诺+报价 → 商务偏差表，技术方案 → 技术偏差表，*号条款 → 关键偏差表）。若用户提供了参考标书，应先读取其归类风格作为参照。

**第二步：回填。** 偏差表从底版空模板的第二行开始逐条追加聚合后的响应行。同步回填底版中所有**填空型模块**：封面（项目名称/投标人名称/日期）、投标函（项目名称/报价/承诺项）、基本情况表、供应商廉洁承诺书、中标服务费承诺书等。

报价表从方案对应分项压缩服务内容。**MUST** 单价/总价由用户提供，**MUST NOT** 编造。

**⛔ BLOCKING**: 价格需用户确认。

```markdown
## ✅ Step 5 Checkpoint
- [x] 聚合判定完成（商务按主题合并，技术按子条款展开，*号以实际标注为准）
- [x] 商务偏差表已回填（共 N 条）
- [x] 技术偏差表已回填（共 M 条）
- [x] 关键偏差表已回填（含*号条款或"无标*号项"说明）
- [x] 引用位置精确到子节编号
- [x] 分项报价表服务内容和备注已从方案压缩填写
- [x] 报价表单价/总价已由用户确认（或标注"待补充"）
- [x] 填空型模块已回填（封面/投标函/承诺书/基本情况表等）
- [ ] **Next**: 最终核查
```

---

## Final Verification

生成完毕后，**MUST** 逐项核对 [`references/checklist.md`](references/checklist.md)：

🔴 **废标项优先**: 项目名称编号一致、保证金满足、资格审查齐全、报价不超限价、星号条款响应、签字盖章完整。

🟡 **格式项**: 封面信息、目录页码、空白页、无其他项目残留。

🟢 **一致性**: 分项名称在方案/偏差表/报价表中一致、偏差表引用位置真实存在、报价合计=投标函总价。

参见 [`references/industry-knowledge.md`](references/industry-knowledge.md) 中"标书检查6轮核对法"。

```markdown
## ✅ Bid Generation Complete
- [x] 最终 docx 已生成
- [x] 废标项检查通过
- [x] 格式检查通过
- [x] 内容一致性检查通过
```

---

## File Structure

```
bid-generator/
├── SKILL.md                                 # 本文件
├── workflows/
│   ├── routing.md                           # 路由判断
│   ├── index.md                             # 工作流注册表
│   ├── failure-recovery.md                  # 故障恢复矩阵
│   ├── quick-reference.md                   # Step 1: 速查表
│   ├── business-technical-split.md          # Step 2: 逐条应答
│   ├── template-extraction.md               # Step 3: 底版提取
│   ├── service-proposal-writing.md          # Step 4: 方案编写
│   └── table-backfill.md                    # Step 5: 偏差报价回填
├── references/
│   ├── artifact-ownership.md                # 产物归属规范
│   ├── format-standards.md                  # 格式规范
│   ├── checklist.md                         # 最终核查清单
│   └── industry-knowledge.md               # 行业经验知识库
├── scripts/
│   └── extract_docx_range.py               # 纯工具：按索引切 docx
└── templates/
```

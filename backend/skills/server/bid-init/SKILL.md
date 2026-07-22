---
name: bid-init
description: 初始化投标项目，导入并冻结输入资料，将招标来源转换为规范 DOCX，完成来源预审，并请用户确认模板来源与章节边界。仅在新项目、初始化阶段失效，或需要从资料接收重新建立可追溯基础时使用。
---

# 前置分析

由招标项目负责人主导，招标需求与合规分析师协作。只建立后续工作可信的资料、规范 DOCX 和模板来源边界；不得识别模板可编辑对象、编写填写说明、撰写投标正文或预填候选 DOCX。

## 进入闸门与阶段产物

适用于新项目创建，或输入资料、来源版本、模板来源与章节边界需要重新建立时。新项目由本 Skill 创建项目目录；已有项目先确认 `work/项目/project.json`、`input/`、`work/` 和 `output/` 属于同一项目根。

| 产物 | 路径 | 负责人 | 用途 |
|---|---|---|---|
| 项目身份 | `work/项目/project.json` | 项目负责人 | 校验项目根与版本 |
| 输入冻结清单 | `work/招标/input-manifest.json` | 项目负责人 | 记录输入文件、类别、冻结时点，以及每份来源的规范 DOCX、转换方式和模板适用性 |
| 模板来源确认 | `work/招标/template-route.json` | 项目负责人 | 仅记录用户确认的模板来源、规范 DOCX 路径和章节边界 |
| 人工预审 | `work/招标/招标文件预审.md` | 合规分析师 | 供用户快速核对的预审结论 |

## 按需加载本地资料

| 触发情形 | 读取内容 |
|---|---|
| 始终 | `references/artifact-ownership.md` |
| 需要从 DOCX/PDF/表格/页脚定位来源 | `references/source-unit-extraction.md` |
| 起草人工预审 | `templates/招标文件预审.md` |
| 创建项目、导入/冻结/核对输入 | `scripts/project_bootstrap.py` |
| 将 `.doc/.rtf/.odt` 转为 DOCX | `scripts/convert_doc_to_docx.py` |
| 规范化并登记招标来源 | `scripts/normalize_tender_to_docx.py` |

先读取本阶段所需参考，不要因“可能有用”一次加载全部下游写作或 DOCX 回填规则。

## 执行流程

### Step 1：建立或打开项目工作区

新项目使用本 Skill 的脚本创建：

```bash
python3 skills/bid-init/scripts/project_bootstrap.py init \
  --workspace-root "<项目工作区父目录>" \
  --project-code "<项目代号>" \
  --project-name "<项目名称>"
```

将命令输出的目录设为 `PROJECT_ROOT`。已有项目直接设置 `PROJECT_ROOT`，并确认 `workspace.yaml`、`input/`、`work/` 和 `output/` 都属于当前项目根。不得把 Skill 安装目录、临时下载目录或其他项目目录当作项目工作区。

### Step 2：导入并冻结输入

将每个用户提供文件按真实用途导入；同一文件可被分析为多种用途，但只在 `input/` 保存一份物理副本。

```bash
python3 skills/bid-init/scripts/project_bootstrap.py import-input --project-root "$PROJECT_ROOT" \
  --category tender --file "<招标文件或澄清文件>"
python3 skills/bid-init/scripts/project_bootstrap.py import-input --project-root "$PROJECT_ROOT" \
  --category bidder --file "<投标方资质或产品资料>"
python3 skills/bid-init/scripts/project_bootstrap.py freeze-inputs --project-root "$PROJECT_ROOT"
python3 skills/bid-init/scripts/project_bootstrap.py verify-inputs --project-root "$PROJECT_ROOT"
```

冻结前检查文件是否能读取、是否存在扫描件/OCR风险、是否有多个版本或补遗、是否缺正文/附件/格式表单。冻结后不得直接替换 `input/` 中的文件；新增或替换资料应重新导入，并使 `init` 及下游阶段失效。

### Step 3：将招标来源规范化为 DOCX

对每一份 `tender` 类冻结输入都生成对应的规范 DOCX，供后续要求提取和模板边界确认使用。`.docx` 复制为规范副本；`.doc`、`.rtf`、`.odt` 使用 LibreOffice 转换；PDF 仅生成可读文本 DOCX，不能作为格式模板提取来源。

```bash
python3 skills/bid-init/scripts/normalize_tender_to_docx.py \
  --project-root "$PROJECT_ROOT" \
  --input "input/<已冻结的招标来源>"
```

逐个运行并检查 `work/招标/input-manifest.json` 中对应条目的 `normalized_docx`、`normalization_engine` 和 `template_eligible`。转换失败、转换后不可打开或 PDF 仅文本副本却被要求作为格式底板时，停止并请求原始可编辑文件或调整模板路线；不得把文本转换件伪装为版式保真模板。

### Step 4：来源预审与风险登记

基于输入冻结清单建立来源关系和预审结论，至少区分：招标公告/采购文件、补遗澄清、格式表单、合同条款、评分办法、投标方材料和仅供参考材料。逐项识别：

1. 正式效力、版本关系和冲突优先级；
2. 项目名称、标包、截止时间、预算/限价、报价方式等关键参数；
3. 资格、否决、签章、密封、格式和递交风险；
4. 评分、技术、商务、合同和售后等后续需要原子化的章节；
5. 无法读取、缺页、疑似矛盾或需要用户澄清的来源问题。

预审报告只陈述可定位事实和风险。不能从格式、历史项目或常识推断本项目要求。

### Step 5：确认模板来源与章节边界

在规范 DOCX 中定位候选模板来源，并向用户展示每个候选的来源、正式性、对应规范 DOCX、起止章节/页码/正文范围、是否包含格式表单，以及推荐理由。此时只确认“从哪里提取、提取到哪里结束”，不识别字段、表格、签章区、页眉页脚或其他模板对象。

| 路线 | 适用条件 | 不可忽略的限制 |
|---|---|---|
| 招标方模板 | 招标资料给出完整模板或内嵌“投标/响应文件格式”章节 | 记录来源 DOCX 和完整文件/章节边界 |
| 用户模板 | 用户明确提供且授权使用 | 记录该文件和确认的使用范围 |
| 自拟模板 | 无可用底板且用户明确同意 | 只记录“无官方来源、允许自拟”；目录与版式设计由 `bid-template` 完成 |

模板来源与章节边界是硬确认点。用户确认后，将确认结果直接写入 `template-route.json`。未确认时停止在本阶段；可以保留预审和候选建议，但不得提取底板或创建自拟 DOCX。

### Step 6：交接给模板阶段

交接内容仅包括：冻结输入、规范 DOCX、来源关系与预审风险、用户确认的模板来源与章节边界。`bid-template` 负责提取底板、验证边界、识别可编辑对象和固定保护范围、编写填写说明、确认大章节前置分页。

确认项目身份、冻结清单、规范 DOCX 信息、模板来源确认和预审报告均已落盘后，交接给 `bid-plan` 与 `bid-template`。

## 阻塞、恢复与完成标准

| 情况 | 处理 |
|---|---|
| 输入文件无法读取或缺关键页 | 在预审中定位文件和影响；请求原件、补页或授权 OCR，不猜测内容。 |
| 补遗与主文件冲突 | 标明冲突来源与优先级依据；无法判断时阻塞并请求用户确认。 |
| 无可用底板 | 推荐自拟路线，但必须等待用户确认。 |
| 冻结后资料变化 | 从 `init` 使下游失效，再重新冻结与预审。 |

完成条件：输入已冻结并可核验；每份招标来源已生成或明确无法生成规范 DOCX；正式来源和风险已预审；用户已确认模板来源与章节边界；输入清单、预审报告和模板来源确认已落盘；台账中的 `init` 为 `completed`。结束时交接下一阶段要读取的规范 DOCX、边界和已知风险。

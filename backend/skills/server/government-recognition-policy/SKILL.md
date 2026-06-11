---
name: government-recognition-policy
description: Chinese government recognition-policy writing guide for drafting, revising, reviewing, and PDF-formatting official documents about recognition, certification, evaluation, and management of centers, platforms, bases, parks, laboratories, demonstration units, and pilot units. Use when Codex needs to write or improve recognition management measures, red-head issuance notices, application guidelines, application forms, construction-plan templates, capability tables, proof templates, commitment letters, performance evaluation measures, annual assessment notices, and related attachments. Focus on guiding content creation and quality review; use the bundled PDF script only after the agent has drafted the actual document content.
metadata:
  tags: [government, policy, pdf, official]
---

# 政府认定管理办法写作技能

用于撰写、修改、审核和排版政府对中心、平台、基地、园区、实验室、示范单位、试点单位等进行认定、评定、建设管理、绩效评价的文件。主产物优先为 PDF。

本 skill 是写作与排版指南，不是固定内容生成器。生成 PDF 前，必须先完成文本创作和质量检查；脚本只渲染已写好的结构化内容。

## 使用流程

1. 明确文件类型、认定对象、主管部门、申报主体、政策目的和附件需求。
2. 读取 `references/writing-guide.md`，确定写作框架、语言边界和材料组织方式。
3. 读取 `references/template-alignment.md`，按范本 PDF 对齐结构和版式。
4. 起草正文和附件内容；事实不明时使用占位符，不虚构依据、文号、金额、期限或部门决定。
5. 使用 `checklists/quality-checklist.md` 检查内容、风险、范本一致性和 PDF 渲染效果。
6. 需要 PDF 时，将已写好的内容整理为 JSON，并调用 `scripts/generate_policy_pdf.py`。

## 优先级

1. 用户本轮明确要求。
2. 用户提供的范本 PDF。
3. 本 skill 的写作指南、范本对齐说明和检查清单。
4. GB/T 9704 等通用公文规则。

范本已经明确的结构和样式，优先按范本处理；通用公文规则只作辅助。

## PDF 工具

```bash
python3 scripts/generate_policy_pdf.py --input policy.json --output out.pdf
```

输入 JSON 应由 agent 先完成内容写作后生成。字段和 block 说明见 `references/json-structure.md`。

脚本默认优先使用 HTML/Chrome 渲染 PDF，以加载 `assets/fonts/` 下的完整字体；没有 Chrome 时回退到 ReportLab。脚本不生成默认章节、默认正文、默认条号、默认红头后缀或默认附件文案。

## 资源导航

- `references/writing-guide.md`：写作技巧、结构选择、语言要求和事实风险。
- `references/template-alignment.md`：按范本 PDF 对齐红头、正文、附件、签署区、日期行和版记。
- `references/patterns.md`：认定管理类文件的常见场景、章节和附件组合。
- `references/json-structure.md`：PDF 输入 JSON 字段、block 类型和渲染效果。
- `checklists/quality-checklist.md`：内容、风险、范本一致性和 PDF 渲染检查项。
- `scripts/generate_policy_pdf.py`：PDF 排版脚本，只渲染已写好的 JSON 内容。
- `assets/fonts/`：PDF 渲染使用的可选字体目录。

---
name: create-word-doc
description: 创建、编辑 .docx 文件（报告、合同、提案）。当需要生成 Word 文档、从头创建文档、编辑现有文档、使用模板填充、提取文本时使用。处理报告、备忘录、合同、邀请函、证书等交付物。
allowed-tools: Bash, Read, Write, Glob, Grep
metadata:
  tags: [docx, word, report, document]
---

# Create Word Doc

将主题、草稿、大纲或现有文档转换为专业的 Word 文档。分为两个相连的阶段：首先设计文档结构和内容，然后生成/编辑 `.docx` 并检查布局。

需要更深入的写作和审阅指导时，请阅读 `references/word-document-workflow.md`。

## 核心原则

- **样式优先**：使用 Word 内置样式（标题、段落、列表）而非手动格式化，确保一致性和可访问性。
- **DOCX 为编辑源**：.docx 是可编辑的源文件格式，发布时保持 .docx 格式。
- **内容优先**：文档应回答用户的核心需求，提供完整内容而非单薄大纲。

## 工具选择矩阵

| 任务 | 工具/库 | 语言 | 适用场景 |
|------|---------|------|----------|
| 创建 DOCX | python-docx | Python | 报告、合同、提案 |
| 创建 DOCX | docx | Node.js | 服务端文档生成、TypeScript 项目 |
| DOCX 转 HTML | mammoth.js | Node.js | Web 展示、内容提取 |
| 解析 DOCX | python-docx | Python | 提取文本、表格、元数据 |
| 模板填充 | docxtpl | Python | 邮件合并、基于模板的生成 |
| 审阅流程 | Word 比较/评论/高亮 | 任意 | 人工审阅，无需 OOXML 手术 |
| 修订记录 | OOXML 检查 | 任意 | 真正的修订痕迹或解析修订记录 |

### 工具选用建议

- **docxtpl**：当非开发人员需要在 Word 中编辑布局/设计时使用。
- **python-docx**：当需要结构性编辑（段落/表格/页眉页脚）且格式复杂度适中时使用。
- **docx** (Node.js)：TypeScript 技术栈的服务端文档生成。
- **mammoth**：纯文本提取或 DOCX 转 HTML（可能丢失部分布局）。

### 已知限制

- `.doc`（旧格式）不被这些库支持；需先转换为 `.docx`（如使用 LibreOffice）。
- `python-docx` 无法可靠地创建真正的修订痕迹；使用 Word 比较或专用 OOXML 工具。
- 目录和很多字段是占位符，需在 Word 中打开/更新后才生效。

## 工作流程

### 1. 明确文档概要

确定最少的必要信息：
- **目的**：告知、说服、决定、记录、培训、销售或合规。
- **受众**：高管、运营人员、工程师、客户、监管机构、学生或普通读者。
- **语气**：正式、简洁、指令性、分析性、说服性或中性。
- **篇幅**：单页备忘录、短报告、标准报告、长篇指南或自定义页/字数。
- **输入**：来源笔记、URL、会议记录、草稿文本、数据表格、品牌/模板文件或示例。
- **约束**：必需语言、保密性、引用标准、审批流程或格式模板。

用户未指定时，根据任务推断保守默认值并简要说明。

### 2. 设计大纲

根据文档用途选择结构：

- **备忘录**：标题、背景、建议、理由、风险、下一步。
- **报告**：封面页、执行摘要、背景、发现、分析、建议、附录。
- **提案**：问题、目标、方法、范围、时间表、交付物、价格/资源计划、验收标准。
- **指南/手册**：概述、前提条件、分步程序、示例、故障排除、参考。
- **政策**：目的、范围、定义、政策声明、角色、程序、例外、审查周期。

长文档每个顶级部分负责一个问题。添加子目录仅在改善导航时使用。

### 3. 充实内容

逐节从大纲扩展到草稿：

- 开门见山，先说要点，再补充解释和证据。
- 将模糊的声明转化为具体的观察、决定、风险或行动。
- 仅在示例能澄清想法如何实际工作时添加。
- 使用表格呈现读者比较的信息：选项、风险、角色、时间表、需求、成本或决定。
- 谨慎使用提示框标注重要说明、假设或依赖。
- 保持来源支持的声明可追溯。无法获取来源时，标记为假设或建议。

避免填充模式：重复介绍、通用好处、未经支持的夸张表述、冗余的结尾段落。

### 4. 创建/编辑 DOCX

#### Python 示例 (python-docx)

```python
from docx import Document
from docx.shared import Inches, Pt, Cm
from docx.enum.text import WD_ALIGN_PARAGRAPH

doc = Document()

# 标题
title = doc.add_heading('文档标题', 0)
title.alignment = WD_ALIGN_PARAGRAPH.CENTER

# 带格式的段落
para = doc.add_paragraph()
run = para.add_run('这是粗体 ')
run.bold = True
run = para.add_run('和斜体文本。')
run.italic = True

# 表格
table = doc.add_table(rows=3, cols=3)
table.style = 'Table Grid'
for i, row in enumerate(table.rows):
    for j, cell in enumerate(row.cells):
        cell.text = f'第 {i+1} 行，第 {j+1} 列'

# 图片
doc.add_picture('image.png', width=Inches(4))

# 保存
doc.save('output.docx')
```

#### 模板填充 (docxtpl)

```python
from docxtpl import DocxTemplate

doc = DocxTemplate('template.docx')
context = {
    'company_name': '某某公司',
    'date': '2025-01-15',
    'items': [
        {'name': '产品 A', 'price': 100},
        {'name': '产品 B', 'price': 200},
    ]
}
doc.render(context)
doc.save('filled_template.docx')
```

#### Node.js 示例 (docx)

```typescript
import { Document, Packer, Paragraph, TextRun, Table, TableRow, TableCell } from 'docx';
import * as fs from 'fs';

const doc = new Document({
  sections: [{
    properties: {},
    children: [
      new Paragraph({
        children: [
          new TextRun({ text: '粗体文本', bold: true }),
          new TextRun({ text: ' 和普通文本。' }),
        ],
      }),
      new Table({
        rows: [
          new TableRow({
            children: [
              new TableCell({ children: [new Paragraph('单元格 1')] }),
              new TableCell({ children: [new Paragraph('单元格 2')] }),
            ],
          }),
        ],
      }),
    ],
  }],
});

Packer.toBuffer(doc).then((buffer) => {
  fs.writeFileSync('output.docx', buffer);
});
```

### 5. 验证交付

- 尽可能渲染或打开文档，检查分页、表格环绕、页眉页脚、图片大小和文本溢出。
- 如果 LibreOffice 可用，用 `soffice --headless --convert-to pdf --outdir <dir> <file.docx>` 转换为 PDF 进行检查。
- 如果 Poppler 可用，用 `pdftoppm -png <file.pdf> <output-prefix>` 渲染 PDF 页面。
- 如果无法视觉渲染，用 `python-docx` 提取文本并明确说明未进行布局验证。

### 6. 编辑现有文档

- 通过复制到新输出路径来保留原始文件。
- 在段落、表格、页眉和页脚中搜索占位符。
- 如果小型目标化 XML 或占位符编辑能保留更多格式，则避免重写整个文档。
- 更改后检查渲染输出，尤其是表格和分页附近。

## 样式参考

| 元素 | Python 方法 | Node.js 类 |
|------|-------------|------------|
| 标题 1 | `add_heading(text, 1)` | `HeadingLevel.HEADING_1` |
| 粗体 | `run.bold = True` | `TextRun({ bold: true })` |
| 斜体 | `run.italic = True` | `TextRun({ italics: true })` |
| 字体大小 | `run.font.size = Pt(12)` | `TextRun({ size: 24 })` (半磅) |
| 对齐 | `WD_ALIGN_PARAGRAPH.CENTER` | `AlignmentType.CENTER` |
| 分页符 | `doc.add_page_break()` | `new PageBreak()` |

## 质量检查清单

- **结构**：一致的标题层级、样式，必要时包括自动生成的目录。
- **决策**：决策/行动包含负责人和截止日期（不埋没在正文中）。
- **版本**：文档 ID + 版本 + 变更摘要；定义审查周期。
- **可访问性**：标题/阅读顺序正确；表格标题已标记；非装饰性图片有替代文本。
- **复用**：使用 `references/doc-template-pack.md` 记录决策日志和 recurring 文档类型。

## 注意事项

### 应该做

- 对长文档使用一致的标题层级和目录。
- 用负责人和截止日期记录决策和行动项。
- 将文档存储在可版本化、可搜索的系统中。

### 避免

- 手动格式化而非样式（破坏一致性）。
- 没有负责人或审查周期的文档（很快过时）。
- 复制/粘贴而不更新定义和链接。

### 禁止

- 伪造引用、签名、审批、客户报价或需要来源支持的声明。
- 使用占位符如 `[插入详情]`，除非用户明确要求可填充草稿。
- 制造虚构事实或 quotes。

### 交付约定

- 文件名描述性强且稳定，如 `output/doc/<主题>-报告.docx`。
- 交付完整文档，非单薄大纲（除非用户明确要求大纲）。
- 第一页有意设计：清晰标题、日期或版本（适当时）、有用的开头摘要。
- 保持排版一致且可读；避免默认模板的杂乱。
- 确保表格在页边距内且有清晰的标题行。

## 导航

**参考资料 (references/)**
- [word-document-workflow.md](references/word-document-workflow.md) - 长篇内容、编辑、润色流程
- [docx-patterns.md](references/docx-patterns.md) - 高级格式、样式、页眉页脚
- [template-workflows.md](references/template-workflows.md) - 邮件合并、批量生成
- [accessibility-compliance.md](references/accessibility-compliance.md) - WCAG 2.2 AA、阅读顺序、替代文本
- [cross-platform-compatibility.md](references/cross-platform-compatibility.md) - Word/Google Docs/LibreOffice 兼容性
- [tracked-changes.md](references/tracked-changes.md) - 修订记录处理与限制
- [document-automation-pipelines.md](references/document-automation-pipelines.md) - CI/CD 批量生成、质量门禁

**脚本工具 (scripts/)**
- [docx_inspect_ooxml.py](scripts/docx_inspect_ooxml.py) - 依赖无关的 OOXML 检查
- [docx_extract.py](scripts/docx_extract.py) - 提取文本/表格到 JSON
- [docx_render_template.py](scripts/docx_render_template.py) - 渲染 docxtpl 模板
- [docx_to_html.mjs](scripts/docx_to_html.mjs) - 转换 .docx 到 HTML

**模板资源 (assets/)**
- [report-template.md](assets/report-template.md) - 标准报告结构
- [contract-template.md](assets/contract-template.md) - 法律文档结构
- [doc-template-pack.md](assets/doc-template-pack.md) - 决策日志、会议记录、变更日志模板

**相关技能**
- `../document-pdf/SKILL.md` - PDF 生成和转换（如需要）
- `../docs-codebase/SKILL.md` - 技术写作模式

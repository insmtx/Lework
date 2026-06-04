# PDF JSON 输入结构说明

`scripts/generate_policy_pdf.py` 是纯排版器。Agent 必须先完成文本创作，再把内容整理为本 JSON 结构。脚本不生成默认正文、不生成默认章节、不内置附件文案。

## 顶层字段

必填：

| 字段 | 类型 | 作用 | 渲染效果 |
|---|---|---|---|
| `authority` | string | 发文机关 | 首页红头、首页落款 |
| `authority_suffix` | string | 红头后缀 | 拼接在发文机关后，例如“文件” |
| `doc_number` | string | 发文字号 | 首页红头下方居中 |
| `notice_title` | string | 通知标题 | 首页红线下方居中标题 |
| `policy_title` | string | 办法标题 | 正文首页居中标题 |
| `issue_date` | string | 成文日期 | 首页右侧落款日期 |
| `chapters` | array | 正文章节 | 按“第X章 + 条款”排版 |

常用可选：

| 字段 | 类型 | 作用 | 渲染效果 |
|---|---|---|---|
| `addressees` | string | 主送单位 | 首页正文前一行 |
| `notice_body` | string | 印发通知正文 | 首页正文段落 |
| `print_office` | string | 印发机关/办公室 | 最后一页印发栏左侧 |
| `print_date` | string | 印发日期 | 最后一页印发栏右侧 |
| `attachments` | array | 附件页 | 按附件序号逐个渲染 |

## 正文章节结构

```json
{
  "chapters": [
    {
      "title": "总则",
      "articles": [
        "第一条  为规范……，制定本办法。",
        "第二条  本办法所称……，是指……。"
      ]
    }
  ]
}
```

效果：

- `title` 渲染为居中的“第X章 标题”。
- `articles` 渲染为正文条款。
- 脚本不会自动补条号。
- `articles` 中必须由 agent 写好完整条款编号和条款文本。

## 附件结构

```json
{
  "attachments": [
    {
      "title": "XX中心能力条件认定申报表",
      "blocks": []
    }
  ]
}
```

效果：

- 每个附件自动显示 `附件 1`、`附件 2`。
- `title` 渲染为附件页居中标题。
- `blocks` 决定附件正文内容。

## Block 类型

### 1. `paragraphs`

```json
{
  "type": "paragraphs",
  "items": [
    "本单位（名称：________________）郑重承诺：",
    "1. 申报材料真实、准确、完整。"
  ]
}
```

效果：逐段渲染为正文段落。

可选字段：

- `style`: `body` 或 `body_no_indent`。默认 `body`。

### 2. `table`

```json
{
  "type": "table",
  "columns": ["能力指标", "支撑材料", "申报单位能力"],
  "widths_mm": [50, 58, 48],
  "rows": [
    ["专业服务能力", "服务清单、项目案例、合同或成果证明", ""],
    ["团队和人才情况", "人员清单、学历/职称证明、社保记录", ""]
  ]
}
```

效果：

- `columns` 渲染为加重表头。
- `rows` 渲染为表格正文。
- `widths_mm` 控制列宽；不填时按可用宽度平均分列。

注意：

- 空字符串 `""` 会保留空白填写格。
- 行数较多时会自然分页，但复杂跨页表格仍需渲染检查。

### 3. `signature`

```json
{
  "type": "signature",
  "labels": ["申报单位盖章：", "填表人：", "联系电话：", "填表日期："]
}
```

效果：

- 在右侧渲染填写项。
- 标签右侧保留空白列，便于手写或后续填写。

### 4. `spacer`

```json
{
  "type": "spacer",
  "height_mm": 8
}
```

效果：插入指定高度的空白。

### 5. `page_break`

```json
{
  "type": "page_break"
}
```

效果：强制分页。

## 最小示例

```json
{
  "authority": "XX市XX局",
  "authority_suffix": "文件",
  "doc_number": "市XX发〔20XX〕XX号",
  "notice_title": "XX市XX局关于印发XX市XX中心认定管理办法（试行）的通知",
  "addressees": "各有关单位：",
  "notice_body": "为规范……，现将《……》印发给你们，请结合实际认真贯彻执行。",
  "policy_title": "XX市XX中心认定管理办法（试行）",
  "issue_date": "20XX年XX月XX日",
  "print_office": "XX市XX局办公室",
  "print_date": "20XX年XX月XX日印发",
  "chapters": [
    {
      "title": "总则",
      "articles": ["第一条  为规范……，制定本办法。"]
    }
  ],
  "attachments": []
}
```

## 带附件示例

```json
{
  "attachments": [
    {
      "title": "XX中心能力条件认定申报表",
      "blocks": [
        {
          "type": "table",
          "columns": ["能力指标", "支撑材料", "申报单位能力"],
          "widths_mm": [50, 58, 48],
          "rows": [
            ["专业服务能力", "服务清单、项目案例、合同或成果证明", ""],
            ["团队和人才情况", "人员清单、学历/职称证明、社保记录", ""]
          ]
        },
        {
          "type": "spacer",
          "height_mm": 8
        },
        {
          "type": "signature",
          "labels": ["申报单位盖章：", "填表人：", "联系电话：", "填表日期："]
        }
      ]
    }
  ]
}
```

## 使用要求

- JSON 只表达已经写好的内容和排版块，不要让脚本补写政策内容。
- 不要依赖任何默认公文文案；红头后缀、条号、标题、日期、正文、附件内容都应显式写入 JSON。
- 渲染器会做轻量排版规范化：正文条号后保留较宽空白，阿拉伯数字编号后保留空格，数字与中文相邻处补空格。
- PDF 生成后必须渲染检查首页、正文页、表格页和最后印发栏页。
- 如果附件字段很多，优先拆成多个附件或多张表，避免单页表格过密。

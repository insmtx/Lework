# 标书格式与排版参考

> 生成 docx 时的格式参考。**优先级：招标文件模板格式 > 行业通用格式。** 有模板的模块以模板样式为准。

## 格式优先级

1. **招标文件提供的模板** — 最高优先级。模板中的表格、字号、排版以原模板为准
2. **招标文件的格式要求** — 如招标文件对字体字号有明确规定，从其规定
3. **行业通用格式** — 仅在招标文件没有明确要求时使用，仅适用于自拟内容

## 行业通用格式参考（仅用于招标文件无明确要求时）

### 页面设置
- 纸张: A4 (210mm × 297mm)
- 页边距: 上2.54cm、下2.54cm、左3.18cm、右3.18cm

### 字体字号
- 一级标题: 宋体/黑体，三号(16pt)，加粗
- 二级标题: 宋体/黑体，小三号(15pt)，加粗
- 正文: 宋体，小四号(12pt)，两端对齐
- 表格内文: 宋体，五号(10.5pt)

### 段落
- 正文: 1.5倍行距，首行缩进2字符

### 封面
- 投标文件封面要素参考招标文件提供的封面模板格式

## python-docx 参考

```python
from docx.shared import Pt, Cm
from docx.enum.text import WD_ALIGN_PARAGRAPH

# 页面
section.page_width = Cm(21)
section.page_height = Cm(29.7)
section.top_margin = Cm(2.54)
section.bottom_margin = Cm(2.54)
section.left_margin = Cm(3.18)
section.right_margin = Cm(3.18)

# 正文
run.font.name = '宋体'
run.font.size = Pt(12)
paragraph.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY
paragraph.paragraph_format.line_spacing = 1.5
```

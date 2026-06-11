# DOCX 高级格式与样式

## 页面设置

### 边距设置 (Python)

```python
from docx.shared import Inches, Cm
from docx.enum.section import WD_ORIENT

section = doc.sections[0]
section.top_margin = Inches(1)
section.bottom_margin = Inches(1)
section.left_margin = Inches(1.25)
section.right_margin = Inches(1.25)

# 横向页面
new_section = doc.add_section()
new_section.orientation = WD_ORIENT.LANDSCAPE
new_section.page_width = Inches(11)
new_section.page_height = Inches(8.5)
```

### 纸张大小

```python
from docx.enum.section import WD_PAPER_SIZE

section.pagesize = WD_PAPER_SIZE.A4  # A4, LETTER, LEGAL, etc.
```

## 字体与段落样式

### 字体样式

```python
from docx.shared import Pt, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH

# 段落样式
para = doc.add_paragraph('文本内容', style='Heading 1')

# 直接设置字体
run = para.add_run('强调文本')
run.font.name = 'Microsoft YaHei'  # 中文字体
run.font.size = Pt(12)
run.font.bold = True
run.font.italic = True
run.font.underline = True
run.font.color.rgb = RGBColor(0, 0, 0)
```

### 段落格式

```python
from docx.shared import Pt

para = doc.add_paragraph()

# 段落格式
para.alignment = WD_ALIGN_PARAGRAPH.JUSTIFY  # 两端对齐
para.paragraph_format.line_spacing = 1.5  # 1.5 倍行距
para.paragraph_format.space_before = Pt(12)  # 段前间距
para.paragraph_format.space_after = Pt(12)    # 段后间距
para.paragraph_format.first_line_indent = Pt(28)  # 首行缩进
```

### 项目符号和编号

```python
# 项目符号
doc.add_paragraph('第一项', style='List Bullet')
doc.add_paragraph('第二项', style='List Bullet')

# 编号
doc.add_paragraph('第一步', style='List Number')
doc.add_paragraph('第二步', style='List Number')

# 自定义编号格式需要使用 OOXML 或 Word UI
```

## 表格高级用法

### 表格样式与对齐

```python
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.oxml.ns import qn

table = doc.add_table(rows=5, cols=3)
table.style = 'Table Grid'  # 内置样式
table.alignment = WD_TABLE_ALIGNMENT.CENTER

# 设置列宽
for cell in table.columns[0].cells:
    cell.width = Inches(2)
```

### 表格单元格格式

```python
from docx.oxml.ns import qn
from docx.shared import RGBColor, Pt

# 获取单元格
cell = table.rows[0].cells[0]

# 单元格内边距
cell._element.get_or_add_tcPr().append(
    '<w:tcMar><w:top w:w="100"/><w:bottom w:w="100"/></w:tcMar>'
)

# 背景色
shading_elm = qn('w:shd')
cell._element.get_or_add_tcPr().set(shading_elm, '008000')  # 绿色背景

# 文本对齐
cell.paragraphs[0].alignment = WD_ALIGN_PARAGRAPH.CENTER
```

### 合并单元格

```python
# 合并单元格
cell_a = table.cell(0, 0)
cell_b = table.cell(0, 2)
cell_a.merge(cell_b)  # 横向合并

# 取消合并（仅限之前合并的）
# cell_a._tc.merge(cell_b._tc)  # 逆向操作不可逆，需重建
```

### 表格重复标题行

```python
# 设置表格标题行在每页重复
table.rows[0].repeat = True

# 注意：此功能依赖于 Word 版本，部分版本可能不生效
```

## 页眉与页脚

### 基础页眉页脚

```python
from docx.enum.text import WD_ALIGN_PARAGRAPH

# 获取页眉
header = section.header
header_para = header.paragraphs[0]
header_para.text = "文档标题 - 页眉"
header_para.alignment = WD_ALIGN_PARAGRAPH.CENTER

# 添加页眉线条
header_para = header.paragraphs[0]
header_para.border(bottom='single', space=15)

# 获取页脚
footer = section.footer
footer_para = footer.paragraphs[0]
footer_para.text = "第 X 页 共 Y 页"
footer_para.alignment = WD_ALIGN_PARAGRAPH.CENTER
```

### 奇偶页不同页眉

```python
# 需要在 Word 中启用"奇偶页不同"
# section.different_first_page_header_footer = True

# 首页页眉
first_header = section.first_page_header
first_header_para = first_header.paragraphs[0]
first_header_para.text = "封面页眉"

# 首页页脚
first_footer = section.first_page_footer
first_footer_para = first_footer.paragraphs[0]
first_footer_para.text = "封面页脚"
```

### 页码

```python
from docx.shared import Pt
from docx.enum.text import WD_ALIGN_PARAGRAPH

# 添加页码字段
footer = section.footer
paragraph = footer.paragraphs[0]

# 当前页/总页数字段
run = paragraph.add_run()
fldChar_begin = run._element
fldChar_begin.set(qn('w:fldChar'), 'begin')
fldChar_begin.set(qn('w:fldCharType'), 'separate')

run2 = paragraph.add_run('Page')
run2.font.size = Pt(12)

instrText = paragraph.add_run()
instrText._element.set(qn('w:fldChar'), 'instrText')
instrText._element.set(qn('w:fldCharType'), 'separate')
instrText.text = 'PAGE  \* MERGEFORMAT'

fldChar_end = paragraph.add_run()
fldChar_end._element.set(qn('w:fldChar'), 'end')

# 简化方式：直接文本
footer_para = footer.paragraphs[0]
footer_para.text = "第 1 页"
```

## 目录 (TOC)

### 添加目录占位符

```python
# 目录是占位符，需要在 Word 中更新
# python-docx 可以添加域但无法渲染

# 添加目录标题
doc.add_heading('目录', 1)

# 添加段落提示用户更新目录
para = doc.add_paragraph()
run = para.add_run('（目录将在 Word 中自动生成，请按 F9 更新）')
run.italic = True
```

### 手动目录（备选）

```python
# 如果需要手动目录，可遍历标题生成
def add_toc_placeholder(doc):
    doc.add_heading('目录', 1)
    for i in range(1, 4):
        para = doc.add_paragraph()
        para.add_run(f'标题 {i} ............................ 第 X 页')
```

## 图片处理

### 插入图片

```python
from docx.shared import Inches, Pt

# 基本插入
doc.add_picture('logo.png', width=Inches(2))

# 指定高度
from docx.shared import Cm
doc.add_picture('photo.jpg', height=Cm(5))

# 带对齐
para = doc.add_paragraph()
run = para.add_run()
run.add_picture('image.png', width=Inches(4))
para.alignment = WD_ALIGN_PARAGRAPH.CENTER

# 浮动图片（高级）
from docx.drawing import Inches as DrawingInches
from docx.enum.text import WD_WRAP_FORMAT

# 需要操作 XML，较复杂，建议使用 Word 模板
```

### 图片替代文本

```python
# 为图片添加替代文本（可访问性）
drawing = doc.add_picture('image.png', width=Inches(4))
drawing._element.nsmap['wp'] = 'http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing'
drawing._element.nsmap['c'] = 'http://schemas.openxmlformats.org/drawingml/2006/main'

# 添加 wp:docPr 元素设置 altTextTitle 和 altTextDescription
# 较复杂，建议通过 Word 模板设置
```

## 分节符与分页

### 分节符

```python
from docx.enum.section import WD_SECTION_START

# 连续分节符（不新页）
new_section = doc.add_section(WD_SECTION_START.CONTINUOUS)

# 新页分节符
new_section = doc.add_section(WD_SECTION_START.NEW_PAGE)

# 奇数页/偶数页分节符
new_section = doc.add_section(WD_SECTION_START.ODD_PAGE)
```

### 分页符

```python
# 简单分页
doc.add_page_break()

# 在特定段落后分页
para = doc.add_paragraph('内容')
para.runs[0].add_break(WD_BREAK.PAGE)
```

## 脚注与尾注

### 添加脚注

```python
from docx.oxml.ns import qn

# 添加脚注引用
para = doc.add_paragraph()
run = para.add_run('这是脚注引用')
run._element.addprevious(
    '<w:footnoteReference w:id="1" xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>'
)

# 注意：脚注内容需要通过 Word OOXML 或 win32com 设置
```

### 添加尾注

```python
# 类似脚注，需要操作 OOXML
# 建议通过 Word 模板处理复杂脚注/尾注
```

## 超链接

### 添加超链接

```python
from docx.oxml.shared import qn
from docx.oxml.ns import nsmap

# 添加超链接
para = doc.add_paragraph()
hyperlink = para.add_run('访问网站')
hyperlink.underline = True
hyperlink.font.color.rgb = RGBColor(0, 0, 255)

# 设置超链接地址
hyperlink._element.rPr.rStyle.set(nsmap['w'], 'Hyperlink')

# 实际链接地址需要通过 OOXML 或 docx-python 扩展库
```

## 样式管理

### 使用文档样式

```python
from docx.enum.style import WD_STYLE_TYPE

# 列出可用样式
for style in doc.styles:
    if style.type == WD_STYLE_TYPE.PARAGRAPH:
        print(style.name)
```

### 创建自定义样式

```python
from docx.styles.styles import Style

# 创建自定义段落样式
style = doc.styles.add_style('MyHeading', WD_STYLE_TYPE.PARAGRAPH)
style.base_style = doc.styles['Heading 1']
style.font.size = Pt(14)
style.font.bold = True
```

### 应用样式

```python
# 应用样式到段落
para = doc.add_paragraph('内容', style='MyHeading')
```

## 文档属性

### 设置文档属性

```python
# 核心属性
core_props = doc.core_properties
core_props.title = '文档标题'
core_props.author = '作者'
core_props.subject = '主题'
core_props.keywords = '关键词1, 关键词2'
core_props.created = datetime.now()
core_props.modified = datetime.now()
```

### 自定义属性

```python
# 扩展属性（需要特定库）
# doc.core_properties.industry = 'Technology'
```

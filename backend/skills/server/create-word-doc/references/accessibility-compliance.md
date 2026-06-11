# 可访问性合规指南

## WCAG 2.2 AA 合规性

### 基本原则

1. **可感知**：信息必须以用户可以感知的方式呈现
2. **可操作**：用户界面组件和导航必须是可操作的
3. **可理解**：信息和用户界面的操作必须是可理解的
4. **健壮**：内容必须足够健壮，可被各种用户代理（包括辅助技术）解释

## 文档可访问性检查清单

### 1. 标题结构

```python
# ✅ 正确：使用层级标题
doc.add_heading('文档标题', 0)      # 标题 1
doc.add_heading('第一章', 1)        # 标题 2
doc.add_heading('第一节', 2)        # 标题 3

# ❌ 错误：使用加粗文本代替标题
para = doc.add_paragraph()
run = para.add_run('第一章')
run.bold = True
```

### 2. 表格标题

```python
from docx.oxml.ns import qn

# 为表格添加标题行
table = doc.add_table(rows=3, cols=3)
table.style = 'Table Grid'

# 标记第一行为标题行
for cell in table.rows[0].cells:
    # 添加背景色区分
    shading_elm = qn('w:shd')
    cell._element.get_or_add_tcPr().set(shading_elm, 'E0E0E0')
    cell.paragraphs[0].runs[0].bold = True
```

### 3. 替代文本

```python
# 图片替代文本（通过 Word 模板设置更可靠）
# 在 Word 中：图片 -> 右键 -> 视图详细介绍 -> 替代文本

# 对于图标和装饰性图片，在 Word 中标记为"装饰性"
```

### 4. 链接文本

```python
# ✅ 清晰描述性链接文本
para = doc.add_paragraph()
run = para.add_run('查看我们的')
run2 = para.add_run('隐私政策')
run2.hyperlink.address = 'https://example.com/privacy'
run2.underline = True

# ❌ 避免："点击这里"、链接 URL 显示
```

### 5. 颜色对比

```python
# 文本颜色对比度要求：
# 正常文本：4.5:1 最小值
# 大文本（18pt+ 或 14pt+ 粗体）：3:1 最小值

from docx.shared import RGBColor

# ✅ 对比度好的颜色组合
# 黑色文字 (#000000) on 白色背景 (#FFFFFF) = 21:1
# 深灰文字 (#333333) on 白色背景 = 12.6:1

# ❌ 对比度不足
# 浅灰文字 (#CCCCCC) on 白色背景 = 1.6:1
```

### 6. 阅读顺序

```python
# 确保内容按逻辑顺序添加
# 标题 -> 段落 -> 列表 -> 表格 -> 图片

# 错误顺序示例
doc.add_picture('image.png')  # 图片在前
doc.add_heading('标题', 1)     # 标题在后

# 正确顺序
doc.add_heading('标题', 1)
doc.add_picture('image.png')
```

## 屏幕阅读器兼容性

### 检查点

| 项目 | 验证方法 |
|------|----------|
| 标题层级 | 使用屏幕阅读器验证导航 |
| 表格结构 | 检查表头和行/列描述 |
| 链接文本 | 朗读链接确认描述性 |
| 图像替代 | 确认图像有替代文本 |
| 阅读顺序 | 按 Tab 键确认焦点顺序 |

### 表格可访问性

```python
# 为复杂表格添加摘要
# Word 中：表格属性 -> 替代文本 -> 标题/描述

# 示例：为数据表添加描述
table = doc.add_table(rows=10, cols=5)
# 在 Word 中设置：
# 标题：销售数据表
# 描述：2024年各地区月度销售数据，包含销售额和增长率
```

### 列表可访问性

```python
# ✅ 使用正确的列表样式
doc.add_paragraph('第一点', style='List Bullet')
doc.add_paragraph('第二点', style='List Bullet')

# ✅ 使用编号列表
doc.add_paragraph('第一步', style='List Number')
doc.add_paragraph('第二步', style='List Number')

# ❌ 避免手动创建列表外观
```

## 欧盟 EAA 可访问性要求

### 2025 年合规要点

1. **电子文档**：公共部门需提供无障碍电子文档
2. **最低要求**：符合 WCAG 2.1 AA 标准
3. **例外情况**：实时数据、存档材料等

### 文档元数据

```python
# 设置可访问性相关元数据
core_props = doc.core_properties
core_props.title = '年度财务报告'
core_props.subject = '2024财年财务数据'
core_props.keywords = '财务, 年度报告, 可访问'

# 自定义属性（如需要）
# 注意：python-docx 对自定义属性支持有限
```

### 语言标记

```python
# 设置文档语言
# Word 中：文件 -> 选项 -> 语言

# 多种语言文档
# 在 Word 中为特定段落设置语言
# 审阅 -> 语言 -> 设置校对语言
```

## 实施指南

### 模板设计

```python
# 可访问性友好模板设计原则

1. # 使用内置样式
doc.add_heading('标题', level=1)  # 避免手动格式化

2. # 保持一致的层级
# 标题1 -> 标题2 -> 标题3

3. # 表格清晰
# - 使用表头行
# - 避免合并复杂单元格
# - 保持简单结构

4. # 链接描述性
# "查看隐私政策" 而非 "点击这里"

5. # 图像说明
# 所有非装饰性图像添加替代文本
```

### 自动化检查

```python
def check_accessibility(doc_path):
    """基础可访问性检查"""
    from docx import Document
    
    doc = Document(doc_path)
    issues = []
    
    # 检查标题层级
    headings = [p for p in doc.paragraphs if p.style.name.startswith('Heading')]
    if not headings:
        issues.append("缺少标题")
    
    # 检查标题层级连续性
    levels = [h.style.name[-1] for h in headings if h.style.name.startswith('Heading ')]
    if levels and max(levels) - min(levels) > 2:
        issues.append("标题层级跳跃过大")
    
    # 检查表格标题
    for table in doc.tables:
        if not table.rows:
            continue
        has_header = any(
            cell.paragraphs[0].runs and cell.paragraphs[0].runs[0].bold
            for cell in table.rows[0].cells
        )
        if not has_header:
            issues.append("表格缺少标题行")
    
    # 检查链接文本
    for para in doc.paragraphs:
        for run in para.runs:
            if hasattr(run, 'hyperlink') and run.hyperlink:
                if not run.text or run.text.lower() in ['click here', 'here', '链接']:
                    issues.append(f"链接文本不具描述性: {run.text}")
    
    return issues
```

## Word 内置辅助功能

### 检查器

```
文件 -> 信息 -> 检查问题 -> 检查辅助功能
```

### 辅助功能检查器

```
审阅 -> 辅助功能检查器
```

### 常见问题修复

| 问题 | 修复方法 |
|------|----------|
| 缺少替代文本 | 图片 -> 右键 -> 视图详细介绍 -> 替代文本 |
| 幻灯片标题 | 确保每张幻灯片有标题 |
| 表格缺少标题 | 表格 -> 表格属性 -> 行 -> 重复标题行 |
| 阅读顺序 | 查看 ->  选择窗格 |
| 空白标题 | 删除或添加描述性标题 |

## 验证工具

### 自动化工具

```bash
# 使用 Python 脚本检查
python scripts/check_accessibility.py document.docx

# 使用 Microsoft Accessibility Checker
# 在 Word 中：审阅 -> 辅助功能检查器

# 使用第三方工具
# - Accessibility Checker Pro
# - CommonLook
# - DAISY
```

### 测试清单

- [ ] 文档可以通过 Tab 键导航
- [ ] 所有图像有替代文本
- [ ] 标题结构清晰
- [ ] 表格有标题行
- [ ] 链接文本描述性强
- [ ] 颜色对比度足够
- [ ] 语言已标记
- [ ] 屏幕阅读器可正确朗读

## 最佳实践

### 1. 模板层面

- 创建可访问性友好的文档模板
- 强制使用内置样式
- 预设替代文本模板

### 2. 创作流程

- 创作时考虑可访问性
- 使用辅助功能检查器
- 发布前验证

### 3. 发布检查

- 使用 Word 辅助功能检查器
- 测试屏幕阅读器兼容性
- 提供替代格式选项

### 4. 文档类型特定

| 文档类型 | 关键可访问性要求 |
|----------|------------------|
| 报告 | 标题、表格描述、图表替代 |
| 合同 | 清晰结构、术语定义 |
| 手册 | 导航链接、步骤编号 |
| 表单 | 标签、说明、错误提示 |

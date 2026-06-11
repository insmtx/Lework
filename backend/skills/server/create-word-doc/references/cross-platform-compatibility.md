# 跨平台兼容性指南

## 支持情况概览

| 应用 | DOCX 支持 | 样式支持 | 表格支持 | 修订记录 | 备注 |
|------|-----------|----------|----------|----------|------|
| Microsoft Word | ✅ 完整 | ✅ 完整 | ✅ 完整 | ✅ 完整 | 参考实现 |
| Google Docs | ✅ 良好 | ⚠️ 部分 | ✅ 良好 | ⚠️ 有限 | 可能丢失部分格式 |
| LibreOffice Writer | ✅ 良好 | ⚠️ 部分 | ✅ 良好 | ⚠️ 有限 | 开源替代 |
| Apple Pages | ✅ 良好 | ⚠️ 部分 | ✅ 良好 | ❌ 不支持 | macOS/iOS |
| WPS Office | ✅ 良好 | ✅ 良好 | ✅ 良好 | ⚠️ 有限 | 国内常用 |

## 常见兼容性问题

### 1. 字体问题

```python
# 问题：指定字体在目标系统上不存在
from docx.shared import Pt

run.font.name = '特殊字体'  # 可能显示为默认字体

# 解决方案：提供回退字体
run.font.name = 'Microsoft YaHei, PingFang SC, sans-serif'
```

### 2. 样式丢失

```python
# 问题：自定义样式在不同应用中的表现
table.style = 'My Custom Style'  # 可能不被其他应用识别

# 解决方案：使用内置样式
table.style = 'Table Grid'  # 通用性更好
```

### 3. 分页差异

```python
# 问题：不同应用对分页符的处理
doc.add_page_break()  # 可能产生不同分页效果

# 解决方案：避免强制分页，使用段落间距
para.paragraph_format.space_after = Pt(12)
```

### 4. 目录兼容性

```python
# 目录是 Word 特有的功能，其他应用可能无法正确渲染

# 解决方案：
# 1. 在 Word 中将目录转换为静态文本
# 2. 提供替代的纯文本大纲
# 3. 使用 HTML 版本替代
```

### 5. 修订痕迹

```python
# python-docx 无法创建真正的修订痕迹

# 解决方案：
# 1. 使用 Word COM 接口（仅 Windows）
# 2. 使用专业库如 docx4j
# 3. 接受跨应用修订不兼容
```

## 跨平台测试清单

### 最小测试矩阵

| 目标平台 | 必测项 |
|----------|--------|
| Word (Windows) | 全部功能 |
| Word (macOS) | 字体、分页 |
| Google Docs | 样式、表格、目录 |
| LibreOffice | 字体、表格、分页 |
| WPS | 全部功能（国内） |

### 自动化测试

```python
# 基础兼容性检查
def check_compatibility(doc_path):
    """检查 DOCX 基本兼容性"""
    import zipfile
    
    required_files = [
        'word/document.xml',
        'word/styles.xml',
    ]
    
    issues = []
    with zipfile.ZipFile(doc_path) as zf:
        for f in required_files:
            if f not in zf.namelist():
                issues.append(f'缺少必需文件: {f}')
    
    return issues
```

## 最佳实践

### 1. 使用通用格式

```python
# ✅ 推荐：通用样式
doc.add_heading('标题', level=1)  # Heading 1
doc.add_paragraph('正文', style='Normal')  # Normal

# ❌ 避免：自定义样式
para = doc.add_paragraph()
para.style = 'MyCustomStyle'
```

### 2. 内嵌字体（高级）

```python
# 仅在需要严格一致性时使用
# 大幅增加文件大小
# 复杂实现，建议通过 Word 模板设置
```

### 3. 提供多种格式

```python
# 为关键文档提供多种格式
# 1. DOCX - 可编辑版本
# 2. PDF - 固定布局
# 3. HTML - Web 版本
```

### 4. 文档说明

```python
# 在文档中说明已知限制
doc.add_paragraph()
run = doc.add_paragraph()
run.add_run('注意：本文档使用 Microsoft Word 创建，建议使用 Word 或 WPS 打开以获得最佳体验。').italic = True
```

## 问题排查

### Google Docs 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 字体变化 | 不支持字体 | 使用标准字体 |
| 表格变形 | 单元格合并方式 | 重新创建表格 |
| 图片丢失 | 嵌入方式 | 使用内联图片 |
| 目录无法更新 | TOC 字段 | 转换为静态文本 |

### LibreOffice 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 样式丢失 | 样式定义差异 | 使用内置样式 |
| 分页错误 | 分节符兼容 | 避免复杂分节 |
| 中文字体 | 默认字体 | 指定中文字体 |

## 检测工具

```bash
# 使用 python-docx 加载测试
python -c "from docx import Document; d = Document('file.docx')"

# 使用 mammoth 转换测试
npx mammoth file.docx --output-format=html

# 使用 LibreOffice 转换测试
soffice --headless --convert-to pdf file.docx
```

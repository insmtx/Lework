# 修订记录（Tracked Changes）

## 什么是修订记录

修订记录（Track Changes）是文档编辑过程中保留所有修改痕迹的功能，包括：
- **插入内容**：新增的文本以特殊颜色或下划线标记
- **删除内容**：删除的文本以删除线标记，保留在文档中
- **格式更改**：格式变化也被跟踪

## 技术限制

### python-docx 的限制

```python
# python-docx 无法创建真正的修订记录
# 只能模拟显示效果

from docx.shared import RGBColor

# 模拟插入（仅视觉效果）
run.font.color.rgb = RGBColor(0, 128, 0)  # 绿色表示新增
run.underline = True

# 模拟删除（仅视觉效果）
run.font.color.rgb = RGBColor(255, 0, 0)  # 红色表示删除
run.font.strike = True
```

### 真正的修订记录需要

| 方法 | 平台 | 难度 |
|------|------|------|
| Word COM API | Windows | 中等 |
| docx4j | 跨平台 | 困难 |
| OpenXML SDK | .NET | 困难 |
| 专业库 | 各种 | 因库而异 |

## 处理策略

### 策略 1：检测修订记录

```python
def has_tracked_changes(doc_path):
    """检测文档是否包含修订记录"""
    import zipfile
    
    with zipfile.ZipFile(doc_path) as zf:
        # 检查相关 XML 标记
        for name in zf.namelist():
            if name.startswith('word/'):
                content = zf.read(name)
                if b'w:ins' in content or b'w:del' in content:
                    return True
    return False

# 使用
if has_tracked_changes('document.docx'):
    print("警告：文档包含修订记录")
```

### 策略 2：接受/拒绝修订（需要 Word）

```python
# 无法通过 python-docx 实现
# 需要使用 Word COM 或其他方法

# 建议：在 Word 中手动操作
# 审阅 -> 接受 -> 接受所有修订
```

### 策略 3：比较文档

```python
# 无法通过 python-docx 实现

# 替代方案：
# 1. 使用 Word 的"比较"功能
# 2. 使用 diff 工具比较提取的文本
# 3. 使用专业库如 python-docx-template
```

## 替代方案

### 方案 1：版本历史

```python
# 保存多个版本
from datetime import datetime
from pathlib import Path

def save_version(content, version, output_dir='output'):
    """保存文档版本"""
    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    filename = f"document_v{version}_{timestamp}.docx"
    Path(output_dir).mkdir(exist_ok=True)
    return Path(output_dir) / filename

# 使用
output_path = save_version(doc, '1.0')
doc.save(output_path)
```

### 方案 2：变更日志

```python
# 在文档中添加变更记录章节

def add_changelog(doc, changes):
    """添加变更日志"""
    doc.add_heading('变更历史', level=2)
    
    table = doc.add_table(rows=1, cols=4)
    table.style = 'Table Grid'
    
    # 表头
    headers = ['版本', '日期', '作者', '变更内容']
    for i, header in enumerate(headers):
        table.rows[0].cells[i].text = header
    
    # 添加变更记录
    for change in changes:
        row = table.add_row()
        row.cells[0].text = change['version']
        row.cells[1].text = change['date']
        row.cells[2].text = change['author']
        row.cells[3].text = change['description']

# 使用
changes = [
    {'version': '1.0', 'date': '2024-01-01', 'author': '张三', 'description': '初始版本'},
    {'version': '1.1', 'date': '2024-01-15', 'author': '李四', 'description': '添加第二章'},
]
add_changelog(doc, changes)
```

### 方案 3：Git 版本控制

```bash
# 将 DOCX 存入 Git
git add document.docx
git commit -m "Add initial document"

# 查看历史
git log --oneline document.docx

# 差异比较（仅文本）
git diff document.docx^ document.docx | strings | grep -v '^Binary'
```

## 最佳实践

### 创建前

1. **明确文档管理策略**：是否需要修订记录
2. **使用模板**：预先设置样式减少后续修改
3. **定期保存版本**：而非依赖修订记录

### 处理修订文档

```python
def process_tracked_document(doc_path):
    """处理包含修订记录的文档"""
    import warnings
    
    if has_tracked_changes(doc_path):
        warnings.warn(
            "文档包含修订记录。"
            "建议在 Word 中接受/拒绝所有修订后再处理。",
            UserWarning
        )
    
    # 继续处理，但提醒用户
    return doc_path
```

### 导出时

```python
def clean_for_export(doc_path, output_path):
    """清理文档准备导出"""
    # 这需要 Word COM 或类似工具
    # 建议在 Word 中手动操作：
    # 1. 审阅 -> 修订 -> 显示标记 -> 关闭
    # 2. 审阅 -> 接受 -> 接受所有修订
    # 3. 文件 -> 另存为
    pass
```

## Word COM 示例（仅 Windows）

```python
# 需要 pywin32
# 仅作参考，谨慎使用

import win32com.client
from pathlib import Path

def accept_all_changes(doc_path):
    """接受所有修订（仅 Windows + Word）"""
    
    word = win32com.client.Dispatch("Word.Application")
    word.Visible = False
    
    doc = word.Documents.Open(Path(doc_path).absolute())
    
    # 接受所有修订
    if doc.Revisions.Count > 0:
        doc.Revisions.AcceptAll()
    
    # 保存
    doc.Save()
    doc.Close()
    word.Quit()
```

## 总结

| 场景 | 建议 |
|------|------|
| 需要真实修订记录 | 使用 Word COM 或接受限制 |
| 只需查看修改 | 保留修订，检测后提醒用户 |
| 版本控制 | 使用 Git + 版本保存 |
| 协作工作流 | 建立接受修订的流程规范 |

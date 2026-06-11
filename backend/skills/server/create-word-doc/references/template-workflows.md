# 模板工作流

## 邮件合并

### 基础邮件合并 (Python)

```python
from docxtpl import DocxTemplate
from openpyxl import load_workbook
from datetime import datetime

# 加载数据源（Excel）
wb = load_workbook('recipients.xlsx')
ws = wb.active

# 读取表头
headers = [cell.value for cell in ws[1]]
print(f"表头: {headers}")

# 加载模板
doc = DocxTemplate('invitation_template.docx')

# 渲染每个收件人
for row in ws.iter_rows(min_row=2, values_only=True):
    # 将行数据转换为字典
    recipient_data = dict(zip(headers, row))
    
    # 添加日期
    recipient_data['date'] = datetime.now().strftime('%Y年%m月%d日')
    
    # 渲染文档
    context = {
        'name': recipient_data.get('name', ''),
        'company': recipient_data.get('company', ''),
        'title': recipient_data.get('title', ''),
        'date': recipient_data.get('date', ''),
    }
    
    doc.render(context)
    
    # 保存输出
    output_filename = f"邀请函_{recipient_data['name']}.docx"
    doc.save(output_filename)
    
    # 重置文档用于下一次渲染
    doc.save('invitation_template.docx')  # 重新加载模板
```

### 使用 CSV 数据源

```python
import csv
from docxtpl import DocxTemplate

doc = DocxTemplate('certificate_template.docx')

with open('recipients.csv', 'r', encoding='utf-8') as f:
    reader = csv.DictReader(f)
    for i, row in enumerate(reader, 1):
        context = {
            'name': row['name'],
            'date': row['date'],
            'award': row['award'],
        }
        doc.render(context)
        doc.save(f"证书_{row['name']}.docx")
        doc = DocxTemplate('certificate_template.docx')  # 重置
```

### 使用 JSON 数据源

```python
import json
from docxtpl import DocxTemplate

doc = DocxTemplate('letter_template.docx')

with open('data.json', 'r', encoding='utf-8') as f:
    data = json.load(f)

for item in data['recipients']:
    context = {
        'name': item['name'],
        'address': item['address'],
        'items': item['items'],  # 列表用于循环
    }
    doc.render(context)
    doc.save(f"信函_{item['name']}.docx")
    doc = DocxTemplate('letter_template.docx')
```

## 批量生成

### 批量生成报告

```python
from docxtpl import DocxTemplate
import os
from datetime import datetime

# 报告数据
reports_data = [
    {
        'title': '2024年度销售报告',
        'period': '2024年1月-12月',
        'revenue': 1000000,
        'growth': '15%',
        'sections': [
            {'heading': '销售业绩', 'content': '...' },
            {'heading': '市场分析', 'content': '...' },
            {'heading': '明年计划', 'content': '...' },
        ]
    },
    # ... 更多报告
]

doc = DocxTemplate('report_template.docx')

for report in reports_data:
    context = {
        'title': report['title'],
        'period': report['period'],
        'generated_date': datetime.now().strftime('%Y年%m月%d日'),
        'revenue': f"{report['revenue']:,}",
        'growth': report['growth'],
        'sections': report['sections'],
    }
    
    doc.render(context)
    
    # 生成文件名
    safe_title = report['title'].replace(' ', '-')
    doc.save(f"output/reports/{safe_title}.docx")
    
    doc = DocxTemplate('report_template.docx')

print(f"生成了 {len(reports_data)} 份报告")
```

### 批量生成合同

```python
from docxtpl import DocxTemplate
import json
from datetime import datetime

with open('contracts.json', 'r', encoding='utf-8') as f:
    contracts = json.load(f)['contracts']

doc = DocxTemplate('contract_template.docx')

successful = 0
for contract in contracts:
    if not contract.get('active', True):
        continue
        
    context = {
        'party_a': contract['party_a'],
        'party_b': contract['party_b'],
        'contract_no': contract['contract_no'],
        'sign_date': contract['sign_date'],
        'expire_date': contract['expire_date'],
        'amount': f"{contract['amount']:,.2f}",
        'amount_cn': contract['amount_cn'],  # 中文大写金额
    }
    
    doc.render(context)
    doc.save(f"output/contracts/{contract['contract_no']}.docx")
    
    doc = DocxTemplate('contract_template.docx')
    successful += 1

print(f"成功生成 {successful} 份合同")
```

## 模板语法

### 变量替换

```jinja2
{{ company_name }}
{{ date }}
{{ amount }}
```

### 条件渲染

```jinja2
{% if vip_customer %}
尊敬的 {{ name }} 会员，
{% else %}
亲爱的 {{ name }} ，
{% endif %}
```

### 循环

```jinja2
{% for item in items %}
{{ loop.index }}. {{ item.name }} - {{ item.price }}元
{% endfor %}
```

### 过滤器

```jinja2
{{ date|format('%Y年%m月%d日') }}
{{ amount|default('0') }}
{{ name|upper }}
{{ name|lower }}
```

### 内联条件

```jinja2
{{ "已过期" if expired else "有效" }}
{{ "是" if confirmed else "否" }}
```

## 模板设计最佳实践

### Placeholder 命名约定

```jinja2
{# 使用下划线命名 #}
{{ company_name }}
{{ contract_date }}

{# 避免 #}
{{ companyname }}
{{ contractdate }}
```

### 分节模板

```jinja2
{# 报告模板 #}
# {{ title }}

生成日期: {{ generated_date }}

## 执行摘要
{{ executive_summary }}

{% for section in sections %}
## {{ section.heading }}
{{ section.content }}

{% endfor %}

---
{{ author }}
```

### 表格模板

```jinja2
{# 表格中的循环 #}
| 序号 | 项目名称 | 金额 |
|------|----------|------|
{% for item in items %}
| {{ loop.index }} | {{ item.name }} | {{ item.amount }} |
{% endfor %}
| | **合计** | **{{ total_amount }}** |
```

### 条件表格行

```jinja2
| 项目 | 状态 |
|------|------|
{% for item in items %}
| {{ item.name }} | {% if item.completed %}✓ 完成{% else %}进行中{% endif %} |
{% endfor %}
```

## 动态图片

### 插入动态图片

```python
from docxtpl import DocxTemplate
from docx.shared import Inches

doc = DocxTemplate('report_with_chart.docx')

# 方法1: 图片作为变量
context = {
    'title': '销售报告',
    'chart_path': 'chart.png',  # 图片路径
}

doc.render(context)
doc.save('output/report.docx')

# 方法2: 动态生成图片（需要 matplotlib 等）
import matplotlib.pyplot as plt

# 生成图表
plt.figure()
plt.plot([1, 2, 3], [1, 4, 9])
plt.savefig('temp_chart.png', dpi=300)
plt.close()

context = {
    'title': '图表报告',
    'chart_path': 'temp_chart.png',
}

doc.render(context)
doc.save('output/chart_report.docx')

# 清理临时文件
import os
os.remove('temp_chart.png')
```

### 动态图表

```python
import matplotlib.pyplot as plt
from docxtpl import DocxTemplate
import io
import os

def generate_and_embed_chart(doc, chart_type, data):
    """生成图表并嵌入文档"""
    plt.figure(figsize=(6, 4))
    
    if chart_type == 'bar':
        plt.bar(data['labels'], data['values'])
    elif chart_type == 'line':
        plt.plot(data['labels'], data['values'])
    elif chart_type == 'pie':
        plt.pie(data['values'], labels=data['labels'])
    
    plt.title(data.get('title', ''))
    
    # 保存到内存
    buf = io.BytesIO()
    plt.savefig(buf, format='png', dpi=150, bbox_inches='tight')
    buf.seek(0)
    
    # 添加到文档
    doc.add_picture(buf, width=Inches(5))
    buf.close()
    plt.close()
```

## 高级技巧

### 动态页眉页脚

```jinja2
{# 在模板中使用 #}
{% if show_confidential %}
<footer>机密文件 - 仅限内部使用</footer>
{% endif %}
```

### 多语言模板

```python
from docxtpl import DocxTemplate

translations = {
    'zh': {
        'title': '报告',
        'date': '日期',
    },
    'en': {
        'title': 'Report',
        'date': 'Date',
    }
}

for lang, trans in translations.items():
    doc = DocxTemplate(f'template_{lang}.docx')
    doc.render(trans)
    doc.save(f'output/report_{lang}.docx')
```

### 模板继承

```jinja2
{# docxtpl 不支持继承，但可以通过模块化实现 #}
{# base.docx 作为主模板 #}
{{ header }}

{% for section in sections %}
## {{ section.title }}
{{ section.content }}
{% endfor %}

{{ footer }}
```

## 错误处理

### 调试模板

```python
from docxtpl import DocxTemplate, jinja2

# 启用调试
doc = DocxTemplate('template.docx')

# 渲染并捕获错误
try:
    doc.render(context)
    doc.save('output.docx')
except jinja2.TemplateSyntaxError as e:
    print(f"模板语法错误: {e}")
except Exception as e:
    print(f"渲染错误: {e}")
```

### 验证数据

```python
def validate_context(context, required_fields):
    """验证上下文数据"""
    missing = []
    for field in required_fields:
        if field not in context or not context[field]:
            missing.append(field)
    
    if missing:
        raise ValueError(f"缺少必需字段: {', '.join(missing)}")
    
    return True

required = ['company_name', 'date', 'items']
validate_context(context, required)
```

## 性能优化

### 批量处理

```python
import time
from docxtpl import DocxTemplate

start_time = time.time()

doc = DocxTemplate('template.docx')

# 预加载数据
with open('data.json', 'r') as f:
    items = json.load(f)['items']

# 批量渲染
for i, item in enumerate(items):
    doc.render(item)
    doc.save(f'output/item_{i}.docx')
    doc = DocxTemplate('template.docx')

elapsed = time.time() - start_time
print(f"生成 {len(items)} 份文档耗时: {elapsed:.2f}秒")
```

### 异步处理

```python
import asyncio
from concurrent.futures import ThreadPoolExecutor

def render_document(template_path, context, output_path):
    """线程安全渲染"""
    doc = DocxTemplate(template_path)
    doc.render(context)
    doc.save(output_path)

async def batch_render(templates, contexts, output_paths):
    loop = asyncio.get_event_loop()
    with ThreadPoolExecutor(max_workers=4) as executor:
        futures = [
            loop.run_in_executor(
                executor, 
                render_document, 
                t, c, o
            )
            for t, c, o in zip(templates, contexts, output_paths)
        ]
        await asyncio.gather(*futures)
```

## 输出管理

### 按日期组织

```python
from datetime import datetime
import os

output_dir = 'output'
date_str = datetime.now().strftime('%Y%m%d')
day_dir = os.path.join(output_dir, date_str)
os.makedirs(day_dir, exist_ok=True)

doc.save(os.path.join(day_dir, 'report.docx'))
```

### 压缩包输出

```python
import zipfile
import os
from datetime import datetime

zip_name = f"reports_{datetime.now().strftime('%Y%m%d')}.zip"

with zipfile.ZipFile(zip_name, 'w') as zipf:
    for file in os.listdir('output'):
        if file.endswith('.docx'):
            zipf.write(os.path.join('output', file), file)

print(f"已创建: {zip_name}")
```

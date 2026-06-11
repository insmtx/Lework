# 文档自动化流水线

## 流水线架构

```
输入源 → 处理引擎 → 质量检查 → 输出交付
   ↓          ↓           ↓          ↓
数据文件    docxtpl     自动化     DOCX/PDF
              /         检查       HTML
          python-docx
```

## 核心组件

### 1. 数据源

```python
# 支持的数据源类型

SOURCES = {
    'json': 'JSON 文件 - 结构化数据',
    'yaml': 'YAML 文件 - 配置数据',
    'csv': 'CSV 文件 - 表格数据',
    'excel': 'Excel 文件 - 复杂表格',
    'api': 'API 端点 - 动态数据',
    'db': '数据库 - 企业数据',
}
```

### 2. 模板引擎

```python
# docxtpl 模板系统

from docxtpl import DocxTemplate

class DocumentEngine:
    def __init__(self, template_path):
        self.template = DocxTemplate(template_path)
    
    def render(self, context):
        self.template.render(context)
        return self.template
    
    def save(self, output_path):
        self.template.save(output_path)
```

### 3. 质量检查

```python
# 自动化质量检查

class QualityGate:
    """质量门禁"""
    
    def __init__(self):
        self.checks = []
    
    def add_check(self, name, func):
        self.checks.append({'name': name, 'func': func})
    
    def run(self, doc_path):
        results = []
        for check in self.checks:
            try:
                passed = check['func'](doc_path)
                results.append({
                    'name': check['name'],
                    'passed': passed,
                })
            except Exception as e:
                results.append({
                    'name': check['name'],
                    'passed': False,
                    'error': str(e),
                })
        
        return results
```

## 批量生成流水线

### 基础流水线

```python
from pathlib import Path
from datetime import datetime
import json

class DocumentPipeline:
    """文档生成流水线"""
    
    def __init__(self, template_dir, output_dir):
        self.template_dir = Path(template_dir)
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
    
    def load_data(self, data_file):
        """加载数据源"""
        with open(data_file, 'r', encoding='utf-8') as f:
            if data_file.endswith('.json'):
                return json.load(f)
            elif data_file.endswith('.yaml'):
                import yaml
                return yaml.safe_load(f)
    
    def generate(self, template_name, data_list, output_prefix=None):
        """批量生成文档"""
        template_path = self.template_dir / template_name
        
        results = []
        for i, data in enumerate(data_list):
            # 生成输出路径
            if output_prefix:
                output_name = f"{output_prefix}_{i+1}.docx"
            else:
                output_name = f"{data.get('name', f'doc_{i+1}')}.docx"
            
            output_path = self.output_dir / output_name
            
            # 渲染并保存
            doc = DocxTemplate(str(template_path))
            doc.render(data)
            doc.save(str(output_path))
            
            results.append({
                'data': data,
                'output': str(output_path),
                'status': 'success',
            })
        
        return results
    
    def generate_summary(self, results):
        """生成汇总报告"""
        total = len(results)
        successful = sum(1 for r in results if r['status'] == 'success')
        
        summary = {
            'total': total,
            'successful': successful,
            'failed': total - successful,
            'timestamp': datetime.now().isoformat(),
        }
        
        # 保存汇总
        summary_path = self.output_dir / 'generation_summary.json'
        with open(summary_path, 'w', encoding='utf-8') as f:
            json.dump(summary, f, ensure_ascii=False, indent=2)
        
        return summary
```

### 使用示例

```python
# 配置流水线
pipeline = DocumentPipeline(
    template_dir='templates',
    output_dir='output/reports'
)

# 加载数据
data = pipeline.load_data('data/reports.json')

# 批量生成
results = pipeline.generate('report_template.docx', data['reports'])

# 生成汇总
summary = pipeline.generate_summary(results)

print(f"成功: {summary['successful']}/{summary['total']}")
```

## 质量检查实现

### 必需字段检查

```python
def check_required_fields(doc_path, required_fields):
    """检查必需字段是否填写"""
    from docx import Document
    
    doc = Document(doc_path)
    full_text = '\n'.join(p.text for p in doc.paragraphs)
    
    missing = []
    for field in required_fields:
        if field not in full_text:
            missing.append(field)
    
    return len(missing) == 0, missing
```

### 样式一致性检查

```python
def check_style_consistency(doc_path):
    """检查样式一致性"""
    from docx import Document
    
    doc = Document(doc_path)
    styles_used = set()
    
    for para in doc.paragraphs:
        if para.style:
            styles_used.add(para.style.name)
    
    # 检查是否使用了非标准样式
    standard_styles = {'Normal', 'Heading 1', 'Heading 2', 'Heading 3', 
                      'List Bullet', 'List Number', 'Quote'}
    
    non_standard = styles_used - standard_styles
    return len(non_standard) == 0, list(non_standard)
```

### 表格完整性检查

```python
def check_tables_complete(doc_path):
    """检查表格是否有空单元格"""
    from docx import Document
    
    doc = Document(doc_path)
    issues = []
    
    for i, table in enumerate(doc.tables):
        for row_idx, row in enumerate(table.rows):
            for col_idx, cell in enumerate(row.cells):
                if not cell.text.strip():
                    issues.append({
                        'table': i,
                        'row': row_idx,
                        'col': col_idx,
                    })
    
    return len(issues) == 0, issues
```

### 链接检查

```python
def check_hyperlinks(doc_path):
    """检查链接有效性"""
    from docx import Document
    import re
    
    doc = Document(doc_path)
    links = []
    
    # 提取超链接（简化实现）
    for para in doc.paragraphs:
        text = para.text
        urls = re.findall(r'https?://[^\s]+', text)
        links.extend(urls)
    
    # 注意：实际检查需要网络请求
    return len(links), links
```

## CI/CD 集成

### GitHub Actions 示例

```yaml
# .github/workflows/generate-docs.yml
name: Generate Documents

on:
  push:
    paths:
      - 'data/**'
      - 'templates/**'

jobs:
  generate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.11'
      
      - name: Install dependencies
        run: |
          pip install docxtpl python-docx
      
      - name: Generate documents
        run: python scripts/batch_generate.py
      
      - name: Run quality checks
        run: python scripts/quality_check.py
      
      - name: Upload artifacts
        uses: actions/upload-artifact@v3
        with:
          name: generated-documents
          path: output/
```

### 本地开发

```bash
# 安装依赖
pip install docxtpl python-docx openpyxl pyyaml

# 运行流水线
python -m pipeline.main

# 仅运行质量检查
python -m pipeline.quality --input output/
```

## 高级模式

### 条件渲染

```jinja2
{# 根据数据类型选择模板 #}
{% if report.type == 'financial' %}
    {% include 'financial_header.docx' %}
{% else %}
    {% include 'standard_header.docx' %}
{% endif %}
```

### 动态模板选择

```python
def select_template(data):
    """根据数据选择模板"""
    template_map = {
        'financial': 'templates/financial_report.docx',
        'technical': 'templates/technical_report.docx',
        'sales': 'templates/sales_report.docx',
    }
    return template_map.get(data.get('type'), 'templates/default.docx')
```

### 并行生成

```python
from concurrent.futures import ThreadPoolExecutor

def parallel_generate(template_path, data_list, max_workers=4):
    """并行生成文档"""
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = [
            executor.submit(generate_single, template_path, data)
            for data in data_list
        ]
        return [f.result() for f in futures]
```

## 监控与日志

### 日志配置

```python
import logging
from datetime import datetime

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler(f'pipeline_{datetime.now().strftime("%Y%m%d")}.log'),
        logging.StreamHandler(),
    ]
)

logger = logging.getLogger(__name__)
```

### 性能监控

```python
import time
from functools import wraps

def timing(func):
    @wraps(func)
    def wrapper(*args, **kwargs):
        start = time.time()
        result = func(*args, **kwargs)
        elapsed = time.time() - start
        logger.info(f"{func.__name__} 完成，耗时 {elapsed:.2f}秒")
        return result
    return wrapper
```

## 错误处理

### 重试机制

```python
from tenacity import retry, stop_after_attempt, wait_exponential

@retry(stop=stop_after_attempt(3), wait=wait_exponential(multiplier=1, min=1, max=10))
def render_with_retry(template, context, output):
    """带重试的渲染"""
    doc = DocxTemplate(template)
    doc.render(context)
    doc.save(output)
```

### 死信队列

```python
def handle_failure(data, error, dead_letter_dir='dlq'):
    """处理失败的任务"""
    import json
    from pathlib import Path
    
    Path(dead_letter_dir).mkdir(exist_ok=True)
    
    failure_record = {
        'data': data,
        'error': str(error),
        'timestamp': datetime.now().isoformat(),
    }
    
    filename = f"failed_{datetime.now().strftime('%Y%m%d_%H%M%S')}.json"
    with open(Path(dead_letter_dir) / filename, 'w') as f:
        json.dump(failure_record, f)
```

## 部署建议

### Docker 化

```dockerfile
FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install -r requirements.txt

COPY . .

CMD ["python", "-m", "pipeline.main"]
```

### Kubernetes Job

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: doc-generator
spec:
  template:
    spec:
      containers:
      - name: generator
        image: doc-generator:latest
        volumeMounts:
        - name: data
          mountPath: /data
        - name: output
          mountPath: /output
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: data-pvc
      - name: output
        persistentVolumeClaim:
          claimName: output-pvc
```

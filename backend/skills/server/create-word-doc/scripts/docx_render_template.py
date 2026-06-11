#!/usr/bin/env python3
"""
DOCX 模板渲染脚本
使用 docxtpl 渲染模板
"""

import json
import sys
from pathlib import Path

try:
    from docxtpl import DocxTemplate
except ImportError:
    print("错误: 需要安装 docxtpl")
    print("运行: pip install docxtpl")
    sys.exit(1)


def render_template(template_path: str, context: dict, output_path: str):
    """渲染模板"""
    doc = DocxTemplate(template_path)
    doc.render(context)
    doc.save(output_path)
    print(f"已生成: {output_path}")


def load_context(context_path: str) -> dict:
    """加载上下文数据"""
    path = Path(context_path)

    if path.suffix == ".json":
        with open(context_path, "r", encoding="utf-8") as f:
            return json.load(f)
    elif path.suffix == ".yaml":
        import yaml

        with open(context_path, "r", encoding="utf-8") as f:
            return yaml.safe_load(f)
    else:
        raise ValueError(f"不支持的格式: {path.suffix}")


def main():
    if len(sys.argv) < 3:
        print("用法:")
        print(
            "  python docx_render_template.py <template.docx> <context.json> [output.docx]"
        )
        print(
            "  python docx_render_template.py <template.docx> -d key=value [output.docx]"
        )
        sys.exit(1)

    template_path = sys.argv[1]

    if not Path(template_path).exists():
        print(f"错误: 模板文件不存在: {template_path}")
        sys.exit(1)

    # 解析参数
    output_path = None
    context = {}

    if sys.argv[2] == "-d":
        # 命令行参数
        for arg in sys.argv[3:]:
            if "=" in arg:
                key, value = arg.split("=", 1)
                context[key] = value
        if len(sys.argv) > 3 and not sys.argv[4].endswith(".docx"):
            output_path = sys.argv[4]
    else:
        # JSON/YAML 文件
        context_path = sys.argv[2]
        if not Path(context_path).exists():
            print(f"错误: 上下文文件不存在: {context_path}")
            sys.exit(1)
        context = load_context(context_path)

        if len(sys.argv) > 3:
            output_path = sys.argv[3]

    # 默认输出文件名
    if not output_path:
        stem = Path(template_path).stem
        output_path = f"{stem}_filled.docx"

    render_template(template_path, context, output_path)


if __name__ == "__main__":
    main()

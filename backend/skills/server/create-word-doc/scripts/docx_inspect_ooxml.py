#!/usr/bin/env python3
"""
DOCX OOXML 检查脚本
无需外部依赖，直接解析 DOCX 的 XML 结构
"""

import sys
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET
from collections import defaultdict


def inspect_docx(docx_path: str) -> dict:
    """检查 DOCX 文件的 OOXML 结构"""

    results = {
        "document_properties": {},
        "styles": [],
        "headings": [],
        "tables": [],
        "images": [],
        "headers": [],
        "footers": [],
        "tracked_changes": False,
        "warnings": [],
    }

    try:
        with zipfile.ZipFile(docx_path, "r") as zf:
            # 读取文档属性
            try:
                core_props = zf.read("docProps/core.xml")
                root = ET.fromstring(core_props)
                ns = {"dc": "http://purl.org/dc/elements/1.1/"}
                results["document_properties"]["title"] = root.find(".//dc:title", ns)
                results["document_properties"]["author"] = root.find(
                    ".//dc:creator", ns
                )
                results["document_properties"]["created"] = root.find(
                    ".//dc:created", ns
                )
                results["document_properties"]["modified"] = root.find(
                    ".//dc:modified", ns
                )
            except KeyError:
                results["warnings"].append("缺少 core.xml (文档属性)")

            # 检查样式
            try:
                styles_xml = zf.read("word/styles.xml")
                root = ET.fromstring(styles_xml)
                for style in root.findall(
                    ".//w:style",
                    ns={
                        "w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
                    },
                ):
                    style_id = style.get(
                        "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}styleId"
                    )
                    style_type = style.get(
                        "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}type"
                    )
                    results["styles"].append({"id": style_id, "type": style_type})
            except KeyError:
                results["warnings"].append("缺少 styles.xml")

            # 检查文档内容
            try:
                document_xml = zf.read("word/document.xml")
                root = ET.fromstring(document_xml)
                ns = {
                    "w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
                }

                # 查找标题
                for para in root.iter(
                    "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}p"
                ):
                    pPr = para.find("w:pPr", ns)
                    if pPr is not None:
                        pStyle = pPr.find("w:pStyle", ns)
                        if pStyle is not None:
                            style_val = pStyle.get(
                                "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}val",
                                "",
                            )
                            if "Heading" in style_val:
                                text = "".join(
                                    t.text or "" for t in para.findall(".//w:t", ns)
                                )
                                level = style_val.replace("Heading", "")
                                results["headings"].append(
                                    {"level": level, "text": text[:50]}
                                )

                # 查找表格
                for table in root.iter(
                    "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}tbl"
                ):
                    rows = len(table.findall(".//w:tr", ns))
                    cols = 0
                    first_row = table.find(".//w:tr", ns)
                    if first_row is not None:
                        cols = len(first_row.findall(".//w:tc", ns))
                    results["tables"].append({"rows": rows, "cols": cols})

                # 检查修订记录
                if (
                    b"revision" in document_xml
                    or b"w:del" in document_xml
                    or b"w:ins" in document_xml
                ):
                    results["tracked_changes"] = True

            except KeyError:
                results["warnings"].append("缺少 document.xml")

            # 检查图片
            try:
                media_files = [f for f in zf.namelist() if f.startswith("word/media/")]
                results["images"] = media_files
            except:
                pass

            # 检查页眉页脚
            try:
                header_files = [f for f in zf.namelist() if f.startswith("word/header")]
                footer_files = [f for f in zf.namelist() if f.startswith("word/footer")]
                results["headers"] = header_files
                results["footers"] = footer_files
            except:
                pass

    except Exception as e:
        results["errors"] = [str(e)]

    return results


def print_report(results: dict):
    """打印检查报告"""

    print("=" * 60)
    print("DOCX OOXML 检查报告")
    print("=" * 60)

    # 文档属性
    print("\n📋 文档属性")
    print("-" * 40)
    props = results.get("document_properties", {})
    for k, v in props.items():
        if v is not None:
            print(f"  {k}: {v.text if hasattr(v, 'text') else v}")

    # 样式统计
    print(f"\n🎨 样式数量: {len(results.get('styles', []))}")
    heading_styles = [
        s for s in results.get("styles", []) if "Heading" in str(s.get("id", ""))
    ]
    print(f"  标题样式: {len(heading_styles)}")

    # 标题结构
    print(f"\n📑 标题结构 ({len(results.get('headings', []))} 个)")
    print("-" * 40)
    for h in results.get("headings", [])[:10]:
        indent = "  " * (int(h.get("level", 1)) - 1)
        print(f"{indent}标题 {h.get('level')}: {h.get('text', '')}")

    # 表格
    print(f"\n📊 表格 ({len(results.get('tables', []))} 个)")
    print("-" * 40)
    for i, t in enumerate(results.get("tables", [])[:5]):
        print(f"  表格 {i + 1}: {t.get('rows')} 行 × {t.get('cols')} 列")
    if len(results.get("tables", [])) > 5:
        print(f"  ... 共 {len(results.get('tables', []))} 个表格")

    # 图片
    images = results.get("images", [])
    print(f"\n🖼️ 图片 ({len(images)} 个)")
    for img in images[:5]:
        print(f"  - {img}")

    # 页眉页脚
    print(f"\n📝 页眉: {len(results.get('headers', []))} 个")
    print(f"📝 页脚: {len(results.get('footers', []))} 个")

    # 修订记录
    if results.get("tracked_changes"):
        print("\n⚠️  警告: 文档包含修订记录")

    # 警告
    warnings = results.get("warnings", [])
    if warnings:
        print("\n⚡ 警告:")
        for w in warnings:
            print(f"  - {w}")

    print("\n" + "=" * 60)


def main():
    if len(sys.argv) < 2:
        print("用法: python docx_inspect_ooxml.py <document.docx>")
        sys.exit(1)

    docx_path = sys.argv[1]
    if not Path(docx_path).exists():
        print(f"错误: 文件不存在: {docx_path}")
        sys.exit(1)

    results = inspect_docx(docx_path)
    print_report(results)


if __name__ == "__main__":
    main()

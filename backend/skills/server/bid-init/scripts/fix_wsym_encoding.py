#!/usr/bin/env python3
"""将 DOCX 中所有 w:sym 元素解码为 w:t 文本。

WPS 生成的 .doc 经 LibreOffice 转换后，部分表格/正文的中文字符会以
<w:sym w:font="宋体" w:char="5185"/> 的形式存储（hex Unicode 码点），
而非标准的 <w:t>内</w:t>。python-docx、lxml XPath `.//w:t/text()`、
以及多数 docx 读取工具都只提取 w:t，导致内容静默丢失。

本脚本对 DOCX 包中的 word/document.xml 做一次后处理：
  - 遍历所有 w:r（run）元素
  - 收集同一 run 内连续的 w:sym 元素
  - 将连续 w:sym 的 hex char 属性解码为 Unicode 字符，合并为一个 w:t
  - 删除原始 w:sym 元素
  - 保留 run 内原有的 w:rPr 格式属性

用法:
    python3 fix_wsym_encoding.py --input path/to/docx --output path/to/fixed.docx
    python3 fix_wsym_encoding.py --input path/to/docx  # 原地修复

退出码: 0=成功, 1=参数/IO错误, 2=无需修复(无w:sym)
"""
from __future__ import annotations

import argparse
import shutil
import sys
import tempfile
import zipfile
from pathlib import Path

from lxml import etree

W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
W = f"{{{W_NS}}}"
NS = {"w": W_NS}


def has_sym_elements(root: etree._Element) -> bool:
    """检查 document.xml 中是否存在 w:sym 元素。"""
    return root.find(f".//{W}sym") is not None


def decode_sym_run(root: etree._Element) -> int:
    """将所有 w:r 中的连续 w:sym 替换为 w:t，返回替换的 sym 总数。"""
    total_replaced = 0

    for run in root.iter(f"{W}r"):
        children = list(run)

        # 收集对称子序列: [(start_index, [sym_elements])]
        groups: list[tuple[int, list[etree._Element]]] = []
        current_group: list[etree._Element] = []
        current_start: int | None = None

        for i, child in enumerate(children):
            if child.tag == f"{W}sym":
                if not current_group:
                    current_start = i
                current_group.append(child)
            else:
                if current_group:
                    groups.append((current_start, current_group))  # type: ignore[arg-type]
                    current_group = []
                    current_start = None
        if current_group:
            groups.append((current_start, current_group))  # type: ignore[arg-type]

        # 从后往前替换，避免索引偏移
        for start_idx, sym_elements in reversed(groups):
            chars = []
            for sym in sym_elements:
                hex_char = sym.get(f"{W}char", "")
                if hex_char:
                    try:
                        chars.append(chr(int(hex_char, 16)))
                    except (ValueError, OverflowError):
                        pass

            if not chars:
                continue

            decoded_text = "".join(chars)
            total_replaced += len(sym_elements)

            # 创建 w:t 元素
            t_elem = etree.SubElement(run, f"{W}t")
            t_elem.text = decoded_text
            t_elem.tail = None
            # 如果文本前后可能有空白，设置 xml:space="preserve"
            if decoded_text != decoded_text.strip():
                t_elem.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")

            # 将 w:t 插入到第一个 sym 的位置
            run.insert(start_idx, t_elem)

            # 删除所有原始 sym
            for sym in sym_elements:
                run.remove(sym)

    return total_replaced


def fix_wsym(input_path: Path, output_path: Path | None = None) -> dict:
    """修复 DOCX 中的 w:sym 编码问题。

    Args:
        input_path: 输入的 DOCX 文件路径
        output_path: 输出路径，None 表示原地覆盖

    Returns:
        包含修复统计信息的字典
    """
    if not input_path.is_file():
        raise FileNotFoundError(f"文件不存在: {input_path}")
    if output_path is None:
        output_path = input_path

    # 在临时目录中解压、修复、重新打包
    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp = Path(tmp_dir)
        extract_dir = tmp / "extracted"

        with zipfile.ZipFile(input_path, "r") as zin:
            zin.extractall(extract_dir)

        doc_xml_path = extract_dir / "word" / "document.xml"
        if not doc_xml_path.is_file():
            raise ValueError("DOCX 中缺少 word/document.xml")

        parser = etree.XMLParser(remove_blank_text=False, recover=False)
        tree = etree.parse(str(doc_xml_path), parser)
        root = tree.getroot()

        if not has_sym_elements(root):
            return {"fixed": False, "sym_count": 0, "message": "无需修复：未发现 w:sym 元素"}

        sym_count = decode_sym_run(root)

        # 写回修改后的 XML
        tree.write(
            str(doc_xml_path),
            xml_declaration=True,
            encoding="UTF-8",
            standalone=False,
        )

        # 重新打包为 DOCX
        if output_path != input_path:
            final_output = output_path
        else:
            final_output = tmp / "fixed.docx"

        with zipfile.ZipFile(final_output, "w", zipfile.ZIP_DEFLATED) as zout:
            for file in sorted(extract_dir.rglob("*")):
                if file.is_file():
                    arcname = file.relative_to(extract_dir)
                    zout.write(file, arcname)

        if output_path == input_path:
            shutil.copy2(str(final_output), str(input_path))

    return {
        "fixed": True,
        "sym_count": sym_count,
        "message": f"已替换 {sym_count} 个 w:sym 元素为 w:t 文本",
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="将 DOCX 中 w:sym 元素解码为 w:t 文本，修复 WPS/LibreOffice 转换导致的中文内容丢失"
    )
    parser.add_argument("--input", required=True, type=Path, help="输入 DOCX 文件路径")
    parser.add_argument("--output", type=Path, default=None, help="输出 DOCX 路径（默认原地覆盖）")
    args = parser.parse_args()

    try:
        result = fix_wsym(args.input, args.output)
    except (FileNotFoundError, ValueError, IOError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    if not result["fixed"]:
        print(result["message"])
        return 2

    print(result["message"])
    target = args.output or args.input
    print(f"输出: {target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
